// package main orchestrates the initialization and lifecycle of the Message Consolidator backend.
// It wires up configurations, initializes external connections, and ensures a safe teardown during exit.
package main

import (
	"context"
	"errors"
	"log"
	"message-consolidator/ai"
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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/whatap/go-api/trace"
)

var cfg *config.Config

func main() {
	lumberjackLogger := logger.InitLogging()
	cfg = config.LoadConfig()
	logger.SetLevel(cfg.LogLevel)

	// Why: Manual WhaTap instrumentation requires explicit trace.Init/Shutdown.
	// `whatap-go-inst` auto-instrumentation used to inject these calls at build time,
	// but we removed that toolchain (gorilla/mux incompatible). Without Init the
	// global `disable` flag stays true and every trace.Start*/Step is a no-op,
	// producing zero transactions in WhaTap (verified 2026-04-25).
	// Empty config map → agent reads whatap.conf for license, server.host, app_name.
	trace.Init(map[string]string{})
	defer trace.Shutdown()

	logEnvDebug()
	store.SetAutoArchiveDays(cfg.AutoArchiveDays)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger.StartLogRotator(ctx, lumberjackLogger)

	if err := store.InitDB(ctx, cfg); err != nil {
		log.Fatalf("DB Init failed: %v", err)
	}
	// Why: applies admin-managed app_settings on top of .env so DB-stored overrides win at boot.
	// Failure is non-fatal — operator config issues should not block startup.
	if err := config.OverlayFromDB(ctx, cfg, store.LoadAllSettings); err != nil {
		logger.Warnf("Failed to overlay DB settings onto config: %v", err)
	}
	logger.SetLevel(cfg.LogLevel)
	store.SetAutoArchiveDays(cfg.AutoArchiveDays)
	if err := store.LoadMetadata(); err != nil {
		logger.Warnf("Failed to load metadata cache: %v", err)
	}
	scanner.Init(cfg)

	reportsSvc, tasksSvc, identityResolver := initAIServices(ctx, cfg)

	api := handlers.NewAPI(cfg, func(email, lang string) {
		var wg sync.WaitGroup
		scanner.Scan(email, lang, &wg)
		wg.Wait()
	}, func() {
		var wg sync.WaitGroup
		scanner.RunAllScans(ctx, &wg)
		wg.Wait()
	}, reportsSvc, tasksSvc, identityResolver)

	srv := setupApp(ctx, cfg, api)

	waitForShutdownSignal()
	cancel()
	gracefulShutdown(srv)
}

//Why: Diagnoses DSN-modifying env vars (TURSO_*/WHATAP_*) at boot. Secrets masked.
func logEnvDebug() {
	logger.Infof("[ENV-DEBUG] Checking environment for DSN modifiers...")
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "TURSO_") && !strings.HasPrefix(env, "WHATAP_") {
			continue
		}
		parts := strings.SplitN(env, "=", 2)
		key, value := parts[0], ""
		if len(parts) > 1 {
			value = parts[1]
		}
		if key == "TURSO_AUTH_TOKEN" || key == "WHATAP_LICENSE" {
			value = "****"
		}
		logger.Infof("[ENV-DEBUG] %s=%s", key, value)
	}
}

//Why: Boots Gemini-backed services lazily; falls back to AI-less TasksService when no API key is configured.
func initAIServices(ctx context.Context, cfg *config.Config) (*services.ReportsService, *services.TasksService, *ai.IdentityResolver) {
	var gClient *ai.GeminiClient
	if cfg.GeminiAPIKey != "" {
		var err error
		gClient, err = ai.NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
		if err != nil {
			logger.Errorf("Failed to initialize GeminiClient for Reports: %v", err)
		}
	}
	if gClient == nil {
		return nil, services.NewTasksService(nil, nil), nil
	}
	transSvc := services.NewTranslationService(gClient)
	summarizer := services.NewFlashSingleSummarizer(gClient)
	reportsCfg := services.ReportConfig{CutoffSize: services.DefaultReportCutoffSize}
	reportsSvc := services.NewReportsService(summarizer, gClient, transSvc, reportsCfg)
	tasksSvc := services.NewTasksService(transSvc, gClient)
	identityResolver := ai.NewIdentityResolver(gClient)
	return reportsSvc, tasksSvc, identityResolver
}

//Why: Blocks until SIGINT/SIGTERM so the orchestration loop can drive a controlled shutdown.
func waitForShutdownSignal() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
}

//Why: Runs disconnect, flush, HTTP drain, and DB close concurrently so one slow dependency cannot stall the others.
func gracefulShutdown(srv *http.Server) {
	logger.Infof("Shutting down server gracefully...")
	shutdownStart := time.Now()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		logger.Infof("[Shutdown] 1/4 Disconnecting external clients (WhatsApp, Telegram)...")
		channels.DisconnectAllWhatsApp()
		channels.DisconnectAllTelegram()
	}()
	go func() {
		defer wg.Done()
		logger.Infof("[Shutdown] 2/4 Flushing in-memory data to Database...")
		if err := store.FlushTokenUsage(context.Background()); err != nil {
			logger.Errorf("Failed to flush token usage during shutdown: %v", err)
		}
		store.FlushAllScanMetadata()
		logger.Infof("[Shutdown] In-memory data flushed successfully.")
	}()
	wg.Wait()

	logger.Infof("[Shutdown] 3/4 Waiting for active HTTP requests to finish (Max 30s)...")
	ctxTimeout, cancelTimeout := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelTimeout()
	if err := srv.Shutdown(ctxTimeout); err != nil {
		logger.Errorf("Server shutdown error: %v", err)
	}

	if db := store.GetDB(); db != nil {
		logger.Infof("[Shutdown] 4/4 Closing database connections...")
		db.Close()
	}

	logger.Infof("Server exited successfully. Total shutdown time: %v", time.Since(shutdownStart))
}

//Why: Encapsulates the wiring of external services, background workers, and HTTP routes to provide a testable, fully configured server instance.
func setupApp(ctx context.Context, cfg *config.Config, api *handlers.API) *http.Server {
	wireWhatsAppHooks(ctx)
	wireTelegramHooks(ctx)
	bootChannelClients(ctx, cfg)

	//Why: Initializes OAuth configurations for third-party integrations (Google/Gmail) and system-wide authentication.
	auth.SetupOAuth(cfg)
	channels.SetupGmailOAuth(cfg)

	go scanner.StartBackgroundScanner(ctx)

	//Why: Registers all application endpoints to the HTTP router, enabling public and private access points.
	r := mux.NewRouter()
	api.RegisterRoutes(r)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Infof("Startup Complete (Server starting on :%s...)", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server error: %v", err)
		}
	}()

	return srv
}

//Why: WhatsApp IoC hooks injected before client boot — UpdateUserWAJID writes back via Background ctx because OnConnected/OnLoggedOut fire from manager goroutines outlasting the boot ctx.
func wireWhatsAppHooks(ctx context.Context) {
	channels.DefaultWAManager.FetchUserWAJID = func(email string) (string, error) {
		u, err := store.GetOrCreateUser(ctx, email, "", "")
		if err != nil {
			return "", err
		}
		return u.WAJID, nil
	}
	channels.DefaultWAManager.OnConnected = func(email, wajid string) {
		if err := store.UpdateUserWAJID(context.Background(), email, wajid); err != nil {
			logger.Warnf("[WA] UpdateUserWAJID(connect) failed for %s: %v", email, err)
		}
	}
	channels.DefaultWAManager.OnLoggedOut = func(email string) {
		if err := store.UpdateUserWAJID(context.Background(), email, ""); err != nil {
			logger.Warnf("[WA] UpdateUserWAJID(logout) failed for %s: %v", email, err)
		}
	}
}

//Why: Telegram IoC mirrors WhatsApp; FetchUserTgSession/OnSessionUpdated bind telegram_sessions, OnConnected/OnLoggedOut persist tg_user_id.
func wireTelegramHooks(ctx context.Context) {
	channels.DefaultTelegramManager.FetchUserTgSession = func(email string) ([]byte, error) {
		return store.GetTelegramSession(ctx, email)
	}
	channels.DefaultTelegramManager.FetchUserTgCreds = func(email string) (int, string, bool) {
		id, hash, ok, err := store.GetTelegramCreds(context.Background(), email)
		if err != nil {
			logger.Warnf("[TG] GetTelegramCreds failed for %s: %v", email, err)
			return 0, "", false
		}
		return id, hash, ok
	}
	channels.DefaultTelegramManager.OnSessionUpdated = func(email string, data []byte) {
		if err := store.UpsertTelegramSession(context.Background(), email, data); err != nil {
			logger.Warnf("[TG] UpsertTelegramSession failed for %s: %v", email, err)
		}
	}
	channels.DefaultTelegramManager.OnConnected = func(email string, userID int64) {
		if err := store.UpdateUserTgID(context.Background(), email, strconv.FormatInt(userID, 10)); err != nil {
			logger.Warnf("[TG] UpdateUserTgID(connect) failed for %s: %v", email, err)
		}
	}
	channels.DefaultTelegramManager.OnLoggedOut = func(email string) {
		if err := store.UpdateUserTgID(context.Background(), email, ""); err != nil {
			logger.Warnf("[TG] UpdateUserTgID(logout) failed for %s: %v", email, err)
		}
		if err := store.DeleteTelegramSession(context.Background(), email); err != nil {
			logger.Warnf("[TG] DeleteTelegramSession failed for %s: %v", email, err)
		}
	}
}

//Why: Init*X owns its own long-lived client lifecycle (teardown via DisconnectAllX in gracefulShutdown), so it intentionally outlives the boot ctx.
func bootChannelClients(ctx context.Context, cfg *config.Config) {
	users, _ := store.GetAllUsers(ctx)
	for _, u := range users {
		go channels.DefaultWAManager.InitWhatsApp(u.Email, cfg)       //nolint:contextcheck // Independent lifecycle owned by WAManager; teardown via DisconnectAllWhatsApp.
		go channels.DefaultTelegramManager.InitTelegram(u.Email, cfg) //nolint:contextcheck // Independent lifecycle owned by TelegramManager; teardown via DisconnectAllTelegram.
	}
}
