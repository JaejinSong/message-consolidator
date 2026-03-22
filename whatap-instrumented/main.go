package main

import (
	"context"
	"log"
	"message-consolidator/auth"
	"message-consolidator/channels"
	"message-consolidator/config"
	"message-consolidator/handlers"
	"message-consolidator/logger"
	"message-consolidator/scanner"
	"message-consolidator/services"
	"message-consolidator/store"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/whatap/go-api/instrumentation/github.com/gorilla/mux/whatapmux"
	"github.com/whatap/go-api/logsink"
	"github.com/whatap/go-api/trace"
)

var cfg *config.Config

func main() {
	trace.Init(nil)
	defer trace.Shutdown()
	log.SetOutput(logsink.GetTraceLogWriter(os.Stderr))
	logger.InitLogging()
	cfg = config.LoadConfig()
	logger.SetLevel(cfg.LogLevel)

	// 백엔드 자동 보관 기준일 예외 처리 (0일일 경우 6일로 강제 적용)
	if cfg.AutoArchiveDays <= 0 {
		cfg.AutoArchiveDays = 6
	}
	store.SetAutoArchiveDays(cfg.AutoArchiveDays)
	if err := store.InitDB(cfg.NeonDBURL); err != nil {
		log.Fatalf("DB Init failed: %v", err)
	}

	// Load Metadata into Memory Cache
	if err := store.LoadMetadata(); err != nil {
		logger.Warnf("Failed to load metadata cache: %v", err)
	}

	// Initialize Scanner (must be before handlers/setupApp that might use scanner functions)
	scanner.Init(cfg)

	// Initialize Handlers
	handlers.Init(cfg)
	handlers.ScanFunc = scanner.Scan
	handlers.FullScanFunc = scanner.RunAllScans

	srv := setupApp(cfg)

	// Graceful Shutdown 설정
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit // OS 시그널(Ctrl+C 또는 컨테이너 종료) 대기

	logger.Infof("Shutting down server gracefully...")
	shutdownStart := time.Now()

	// 1. 웹소켓 및 외부 연결 안전하게 종료 (WhatsApp)
	logger.Infof("[Shutdown] 1/4 Disconnecting external clients (WhatsApp)...")
	channels.DisconnectAllWhatsApp()

	// 2. 메모리에 남은 지연 쓰기(Lazy Write) 데이터들 강제 플러시
	logger.Infof("[Shutdown] 2/4 Flushing in-memory data to Database...")
	if err := store.FlushTokenUsage(); err != nil {
		logger.Errorf("Failed to flush token usage during shutdown: %v", err)
	}
	store.FlushAllScanMetadata()
	if err := services.FlushGamificationData(); err != nil {
		logger.Errorf("Failed to flush gamification data during shutdown: %v", err)
	}
	logger.Infof("[Shutdown] In-memory data flushed successfully.")

	// 3. 실행 중인 HTTP 요청이 끝날 때까지 최대 5초 대기 후 서버 종료
	logger.Infof("[Shutdown] 3/4 Waiting for active HTTP requests to finish (Max 5s)...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Errorf("Server shutdown error: %v", err)
	}

	// 4. DB 커넥션 풀 완전 종료 (NeonDB 자원 즉시 반환)
	if db := store.GetDB(); db != nil {
		logger.Infof("[Shutdown] 4/4 Closing database connections...")
		db.Close()
	}

	logger.Infof("Server exited successfully. Total shutdown time: %v", time.Since(shutdownStart))
}

// setupApp encapsulates the complex initialization logic for various services and starts the HTTP server.
func setupApp(cfg *config.Config) *http.Server {
	// Setup WhatsApp Manager Callbacks to Decouple DB dependencies
	channels.DefaultWAManager.FetchUserWAJID = func(email string) (string, error) {
		u, err := store.GetOrCreateUser(email, "", "")
		if err != nil {
			return "", err
		}
		return u.WAJID, nil
	}
	channels.DefaultWAManager.OnConnected = func(email, wajid string) {
		store.UpdateUserWAJID(email, wajid)
	}
	channels.DefaultWAManager.OnLoggedOut = func(email string) {
		store.UpdateUserWAJID(email, "")
	}

	// Initialize WhatsApp for all existing users
	users, _ := store.GetAllUsers()
	for _, u := range users {
		go channels.DefaultWAManager.InitWhatsApp(u.Email, cfg.NeonDBURL, cfg)
	}

	// Initialize OAuth
	auth.SetupOAuth(cfg)
	channels.SetupGmailOAuth(cfg)

	// Start Background Workers (Only if NOT in Cloud Run mode)
	if !cfg.CloudRunMode {
		go scanner.StartBackgroundScanner()
	} else {
		logger.Infof("Cloud Run Mode: Background scanner disabled. Triggers via API expected.")
	}

	// Create a new router
	r := whatapmux.WrapRouter(mux.NewRouter())
	handlers.RegisterRoutes(r)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	go func() {
		logger.Infof("기동 완료 (Server starting on :%s...)", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	return srv
}
