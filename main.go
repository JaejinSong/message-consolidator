// package main orchestrates the initialization and lifecycle of the Message Consolidator backend.
// It wires up configurations, initializes external connections, and ensures a safe teardown during exit.
package main

import (
	"context"
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
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"
)

var cfg *config.Config

func main() {
	logger.InitLogging()
	cfg = config.LoadConfig()
	logger.SetLevel(cfg.LogLevel)

	//Why: Synchronizes the store-level auto-archive policy with the central environment configuration to ensure consistent task auditing across the system.
	store.SetAutoArchiveDays(cfg.AutoArchiveDays)

	if err := store.InitDB(cfg); err != nil {
		log.Fatalf("DB Init failed: %v", err)
	}

	//Why: Pre-warms the in-memory cache with frequently accessed metadata (e.g., users, aliases) to minimize database I/O latency on high-traffic paths.
	if err := store.LoadMetadata(); err != nil {
		logger.Warnf("Failed to load metadata cache: %v", err)
	}

	//Why: Initializes the scanner subsystem early because HTTP handlers inject scanner function pointers as dependencies, requiring a strict boot order.
	scanner.Init(cfg)

	//Why: Initializes the AI-powered reporting service with a dedicated Gemini client for business intelligence generation.
	var gClient *ai.GeminiClient
	if cfg.GeminiAPIKey != "" {
		var err error
		gClient, err = ai.NewGeminiClient(context.Background(), cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
		if err != nil {
			logger.Errorf("Failed to initialize GeminiClient for Reports: %v", err)
		}
	}
	var reportsSvc *services.ReportsService
	if gClient != nil {
		summarizer := services.NewFlashSingleSummarizer(gClient)
		config := services.ReportConfig{CutoffSize: 8000}
		reportsSvc = services.NewReportsService(summarizer, gClient, config)
	}

	//Why: Creates the API handler structure with explicit dependency injection, simplifying unit testing and mock substitution.
	api := handlers.NewAPI(cfg, scanner.Scan, func() {
		var wg sync.WaitGroup
		scanner.RunAllScans(context.Background(), &wg)
		wg.Wait()
	}, reportsSvc)

	ctx, cancel := context.WithCancel(context.Background())

	srv := setupApp(cfg, api, ctx)

	//Why: Traps termination signals (SIGINT/SIGTERM) to orchestrate a graceful shutdown, ensuring in-flight messages are processed and database states are consistent.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit //Why: Blocks the main goroutine to keep the application alive until a termination signal is manually or systemically triggered.

	cancel() //Why: Triggers the cancellation of background worker contexts to stop message scanning and event listeners during shutdown.

	logger.Infof("Shutting down server gracefully...")
	shutdownStart := time.Now()

	//Why: Executes cleanup tasks concurrently to prevent one slow dependency (e.g., a lagging WhatsApp WebSocket) from delaying the entire shutdown process.
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		logger.Infof("[Shutdown] 1/4 Disconnecting external clients (WhatsApp)...")
		channels.DisconnectAllWhatsApp()
	}()

	go func() {
		defer wg.Done()
		logger.Infof("[Shutdown] 2/4 Flushing in-memory data to Database...")
		if err := store.FlushTokenUsage(); err != nil {
			logger.Errorf("Failed to flush token usage during shutdown: %v", err)
		}
		store.FlushAllScanMetadata()
		if err := services.FlushGamificationData(); err != nil {
			logger.Errorf("Failed to flush gamification data during shutdown: %v", err)
		}
		logger.Infof("[Shutdown] In-memory data flushed successfully.")
	}()

	wg.Wait()

	//Why: [Shutdown 3/4] Drains in-flight HTTP requests with a bounded 5s timeout to allow active sessions to complete without hanging the process indefinitely.
	logger.Infof("[Shutdown] 3/4 Waiting for active HTTP requests to finish (Max 5s)...")
	ctxTimeout, cancelTimeout := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelTimeout()
	if err := srv.Shutdown(ctxTimeout); err != nil {
		logger.Errorf("Server shutdown error: %v", err)
	}

	//Why: [Shutdown 4/4] Explicitly releases all database connections back to the Turso server cleanly to prevent connection leaks.
	if db := store.GetDB(); db != nil {
		logger.Infof("[Shutdown] 4/4 Closing database connections...")
		db.Close()
	}

	logger.Infof("Server exited successfully. Total shutdown time: %v", time.Since(shutdownStart))
}

//Why: Encapsulates the wiring of external services, background workers, and HTTP routes to provide a testable, fully configured server instance.
func setupApp(cfg *config.Config, api *handlers.API, ctx context.Context) *http.Server {
	//Why: Injects WhatsApp manager callback hooks as an Inversion of Control (IoC) pattern to break circular dependency cycles between the 'channels' and 'store' packages.
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

	//Why: Boots WhatsApp client sessions asynchronously for all registered users to ensure the main server startup remains non-blocking.
	users, _ := store.GetAllUsers()
	for _, u := range users {
		go channels.DefaultWAManager.InitWhatsApp(u.Email, cfg)
	}

	//Why: Initializes OAuth configurations for third-party integrations (Google/Gmail) and system-wide authentication.
	auth.SetupOAuth(cfg)
	channels.SetupGmailOAuth(cfg)

	//Why: Disables the background polling loop in Cloud Run environments to optimize compute costs, relying on external API triggers or Cloud Scheduler schedules.
	if !cfg.CloudRunMode {
		go scanner.StartBackgroundScanner(ctx)
	} else {
		logger.Infof("Cloud Run Mode: Background scanner disabled. Triggers via API expected.")
	}

	//Why: Registers all application endpoints to the HTTP router, enabling public and private access points.
	r := mux.NewRouter()
	api.RegisterRoutes(r)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	go func() {
		logger.Infof("Startup Complete (Server starting on :%s...)", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	return srv
}
