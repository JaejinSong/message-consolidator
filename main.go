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
	"message-consolidator/store"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
)

var cfg *config.Config

func main() {
	logger.InitLogging()
	cfg = config.LoadConfig()
	logger.SetLevel(cfg.LogLevel)

	// Initialize DB
	store.SetAutoArchiveDays(cfg.AutoArchiveDays)
	if err := store.InitDB(cfg.NeonDBURL); err != nil {
		log.Fatalf("DB Init failed: %v", err)
	}

	// Load Metadata into Memory Cache
	if err := store.LoadMetadata(); err != nil {
		logger.Warnf("Failed to load metadata cache: %v", err)
	}

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

	// Initialize Scanner
	scanner.Init(cfg)

	// Initialize Handlers
	handlers.Init(cfg)
	handlers.ScanFunc = scanner.Scan
	handlers.FullScanFunc = scanner.RunAllScans

	// Start Background Workers (Only if NOT in Cloud Run mode)
	if !cfg.CloudRunMode {
		go scanner.StartBackgroundScanner()
	} else {
		logger.Infof("Cloud Run Mode: Background scanner disabled. Triggers via API expected.")
	}

	// Create a new router
	r := mux.NewRouter()

	// Auth Endpoints
	r.HandleFunc("/auth/login", auth.HandleGoogleLogin).Methods("GET")
	r.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		auth.HandleGoogleCallback(w, r, cfg.SlackToken, func(email string) (string, string, error) {
			sc := channels.NewSlackClient(cfg.SlackToken)
			slackUser, err := sc.LookupUserByEmail(email)
			if err != nil {
				return "", "", err
			}
			return slackUser.ID, slackUser.RealName, nil
		})
	}).Methods("GET")
	r.HandleFunc("/auth/logout", auth.HandleLogout).Methods("GET")

	// Protected Static Files
	fs := http.FileServer(http.Dir("./static"))
	r.PathPrefix("/static/").Handler(auth.AuthMiddleware(http.StripPrefix("/static/", fs)))
	r.Handle("/", auth.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFile(w, r, "./static/index.html")
			return
		}
		fs.ServeHTTP(w, r)
	})))

	// Protected API Endpoints
	r.Handle("/api/messages", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleGetMessages))).Methods("GET")
	r.Handle("/api/messages/done", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleMarkDone))).Methods("POST")
	r.Handle("/api/messages/delete", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleDelete))).Methods("POST")
	r.Handle("/api/messages/hard-delete", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleHardDelete))).Methods("POST")
	r.Handle("/api/messages/restore", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleRestore))).Methods("POST")
	r.Handle("/api/messages/archive", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleGetArchived))).Methods("GET")
	r.Handle("/api/messages/archive/count", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleGetArchivedCount))).Methods("GET")
	r.Handle("/api/messages/export", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleExportArchive))).Methods("GET")
	r.Handle("/api/messages/export/excel", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleExportExcel))).Methods("GET")
	r.Handle("/api/messages/export/json", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleExportJSON))).Methods("GET")
	r.Handle("/api/messages/update", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleUpdateTask))).Methods("POST")
	r.Handle("/api/messages/{id:[0-9]+}/original", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleGetOriginal))).Methods("GET")
	r.Handle("/api/user/info", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleUserInfo))).Methods("GET")
	r.Handle("/api/whatsapp/qr", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleWhatsAppQR))).Methods("GET")
	r.Handle("/api/whatsapp/status", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleWhatsAppStatus))).Methods("GET")
	r.Handle("/api/slack/status", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleSlackStatus))).Methods("GET")
	r.Handle("/api/scan", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleManualScan))).Methods("GET")
	r.HandleFunc("/api/internal/scan", handlers.HandleInternalScan).Methods("GET")
	r.Handle("/api/translate", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleTranslate))).Methods("POST")
	r.Handle("/api/user/aliases", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleGetUserAliases))).Methods("GET")
	r.Handle("/api/user/alias/add", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleAddAlias))).Methods("POST")
	r.Handle("/api/user/alias/delete", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleDeleteAlias))).Methods("POST")
	r.Handle("/api/tenant/aliases", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleGetTenantAliases))).Methods("GET")
	r.Handle("/api/tenant/alias/add", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleAddTenantAlias))).Methods("POST")
	r.Handle("/api/tenant/alias/delete", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleDeleteTenantAlias))).Methods("POST")
	r.Handle("/api/user/token-usage", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleGetTokenUsage))).Methods("GET")
	r.Handle("/api/contacts/mappings", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleGetMappings))).Methods("GET")
	r.Handle("/api/contacts/mapping/add", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleAddMapping))).Methods("POST")
	r.Handle("/api/contacts/mapping/delete", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleDeleteMapping))).Methods("POST")
	r.Handle("/api/admin/reclassify", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleReclassifyOldData))).Methods("GET")
	r.Handle("/api/admin/restore-gmail-cc", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleRestoreGmailCC))).Methods("GET")
	r.Handle("/api/release-notes", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleGetReleaseNotes))).Methods("GET")

	// Gmail OAuth Endpoints
	r.Handle("/auth/gmail/connect", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleGmailConnect))).Methods("GET")
	r.HandleFunc("/auth/gmail/callback", handlers.HandleGmailCallback).Methods("GET")
	r.Handle("/api/gmail/status", auth.AuthMiddleware(http.HandlerFunc(handlers.HandleGmailStatus))).Methods("GET")

	// Attach the router to the default http server
	http.Handle("/", r)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	logger.Infof("기동 완료 (Server starting on :%s...)", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: nil,
	}

	go func() {
		logger.Infof("기동 완료 (Server starting on :%s...)", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Graceful Shutdown 설정
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit // OS 시그널(Ctrl+C 또는 컨테이너 종료) 대기

	logger.Infof("Shutting down server gracefully...")

	// 1. 웹소켓 및 외부 연결 안전하게 종료 (WhatsApp)
	channels.DisconnectAllWhatsApp()

	// 2. 메모리에 남은 지연 쓰기(Lazy Write) 데이터들 강제 플러시
	if err := store.FlushTokenUsage(); err != nil {
		logger.Errorf("Failed to flush token usage during shutdown: %v", err)
	}
	store.FlushAllScanMetadata()
	logger.Infof("In-memory metadata flushed successfully.")

	// 3. 실행 중인 HTTP 요청이 끝날 때까지 최대 5초 대기 후 서버 종료
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Errorf("Server shutdown error: %v", err)
	}

	// 4. DB 커넥션 풀 완전 종료 (NeonDB 자원 즉시 반환)
	if db := store.GetDB(); db != nil {
		logger.Infof("Closing database connections...")
		db.Close()
	}

	logger.Infof("Server exited successfully")
}
