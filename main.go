// package main orchestrates the initialization and lifecycle of the Message Consolidator backend.
// It wires up configurations, initializes external connections, and ensures a safe teardown during exit.
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

	// Initialize store-level auto-archive days from the central configuration.
	store.SetAutoArchiveDays(cfg.AutoArchiveDays)

	if err := store.InitDB(cfg); err != nil {
		log.Fatalf("DB Init failed: %v", err)
	}

	// Pre-warm the in-memory cache with frequently accessed metadata (e.g., users, aliases)
	// to minimize database I/O latency on hot paths.
	if err := store.LoadMetadata(); err != nil {
		logger.Warnf("Failed to load metadata cache: %v", err)
	}

	// Initialize the scanner subsystem early. This strict ordering is required
	// because HTTP handlers inject scanner function pointers as dependencies.
	scanner.Init(cfg)

	// Create the API struct with all its dependencies for explicit injection.
	api := handlers.NewAPI(cfg, scanner.Scan, func() {
		var wg sync.WaitGroup
		scanner.RunAllScans(&wg)
		wg.Wait()
	})

	ctx, cancel := context.WithCancel(context.Background())

	srv := setupApp(cfg, api, ctx)

	// Trap termination signals (e.g., SIGINT, SIGTERM from Docker/Cloud Run)
	// to orchestrate a graceful shutdown, ensuring no in-flight data is lost.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit // Block the main goroutine until a termination signal is received.

	cancel() // trigger graceful shutdown for background tasks

	logger.Infof("Shutting down server gracefully...")
	shutdownStart := time.Now()

	// Step 1: Safely terminate active WebSocket sessions (e.g., WhatsApp)
	// to prevent client-side hanging or zombie connections.
	logger.Infof("[Shutdown] 1/4 Disconnecting external clients (WhatsApp)...")
	channels.DisconnectAllWhatsApp()

	// Step 2: Flush all buffered, lazy-written state (token usage, scan metadata, gamification)
	// to the database to guarantee data integrity.
	logger.Infof("[Shutdown] 2/4 Flushing in-memory data to Database...")
	if err := store.FlushTokenUsage(); err != nil {
		logger.Errorf("Failed to flush token usage during shutdown: %v", err)
	}
	store.FlushAllScanMetadata()
	if err := services.FlushGamificationData(); err != nil {
		logger.Errorf("Failed to flush gamification data during shutdown: %v", err)
	}
	logger.Infof("[Shutdown] In-memory data flushed successfully.")

	// Step 3: Drain in-flight HTTP requests. A bounded timeout is applied
	// to prevent the shutdown process from hanging indefinitely.
	logger.Infof("[Shutdown] 3/4 Waiting for active HTTP requests to finish (Max 5s)...")
	ctxTimeout, cancelTimeout := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelTimeout()
	if err := srv.Shutdown(ctxTimeout); err != nil {
		logger.Errorf("Server shutdown error: %v", err)
	}

	// Step 4: Release database connections back to the Turso server cleanly.
	if db := store.GetDB(); db != nil {
		logger.Infof("[Shutdown] 4/4 Closing database connections...")
		db.Close()
	}

	logger.Infof("Server exited successfully. Total shutdown time: %v", time.Since(shutdownStart))
}

// setupApp encapsulates the wiring of external services, background workers,
// and HTTP routes, returning a fully configured server instance.
func setupApp(cfg *config.Config, api *handlers.API, ctx context.Context) *http.Server {
	// Inject callback hooks into the WhatsApp manager. This inversion of control
	// breaks the cyclic dependency between the 'channels' and 'store' packages.
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

	// Asynchronously boot up WhatsApp client sessions for all registered users
	// so that the main server startup is not blocked.
	users, _ := store.GetAllUsers()
	for _, u := range users {
		go channels.DefaultWAManager.InitWhatsApp(u.Email, cfg)
	}

	// Configure OAuth providers for user authentication and Gmail scanning.
	auth.SetupOAuth(cfg)
	channels.SetupGmailOAuth(cfg)

	// In Cloud Run (serverless) mode, background polling is disabled to save compute costs.
	// The system relies on external API triggers (e.g., Cloud Scheduler) instead.
	if !cfg.CloudRunMode {
		go scanner.StartBackgroundScanner(ctx)
	} else {
		logger.Infof("Cloud Run Mode: Background scanner disabled. Triggers via API expected.")
	}

	// Initialize the HTTP router and register all API endpoints.
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
