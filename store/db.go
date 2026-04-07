package store

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/config"
	"message-consolidator/logger"
	"net/url"
	"strings"
	"time"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

var (
	db *sql.DB
)

func InitDB(cfg *config.Config) error {
	var err error
	dbURL := cfg.TursoURL
	authToken := cfg.TursoToken

	//Why: Handles remote-only Turso connections using the libsql:// prefix to ensure proper authentication.
	if strings.HasPrefix(dbURL, "libsql://") && authToken != "" {
		dbURL = fmt.Sprintf("%s?authToken=%s", dbURL, authToken)
	}

	//Why: Configures embedded replicas to support local edge synchronization via the file: prefix and SyncURL settings.
	if strings.HasPrefix(dbURL, "file:") && cfg.TursoSyncURL != "" {
		u, parseErr := url.Parse(dbURL)
		if parseErr == nil {
			q := u.Query()
			q.Set("sync_url", cfg.TursoSyncURL)
			if authToken != "" {
				q.Set("authToken", authToken)
			}
			if cfg.TursoSyncInterval != "" {
				q.Set("sync_interval", cfg.TursoSyncInterval)
			}
			u.RawQuery = q.Encode()
			dbURL = u.String()
			logger.Infof("[DB] Embedded Replica mode enabled: %s (Sync with %s)", u.Path, cfg.TursoSyncURL)
		}
	}

	maskedURL := dbURL
	if idx := strings.Index(maskedURL, "authToken="); idx != -1 {
		maskedURL = maskedURL[:idx+10] + "****"
	}
	logger.Infof("[DB-DEBUG] Opening database with DSN: %s", maskedURL)

	db, err = sql.Open("libsql", dbURL)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	setupConnectionPool(dbURL)

	// Why: Perform critical schema migrations (e.g., adding language_code) BEFORE creating tables or views
	// that might refer to these columns, preventing "no such column" errors during view creation.
	if err := runMigrations(); err != nil {
		logger.Warnf("[DB-INIT] Pre-migration warning: %v", err)
	}

	// Why: Create or update core tables and views. Must return error on failure to ensure data integrity.
	if err := createCoreTables(); err != nil {
		return fmt.Errorf("core table creation failed: %w", err)
	}

	if err := setupGamification(); err != nil {
		return fmt.Errorf("gamification setup failed: %w", err)
	}

	createIndexes()

	return RefreshAllCaches(context.Background())
}

func setupConnectionPool(connStr string) {
	//Why: Configures connection pool settings to prevent "stream is closed" or "bad connection" errors in serverless environments like Turso.
	idleConns := 2
	if strings.HasPrefix(connStr, "libsql://") {
		//Why: Disables idle connections for remote Turso environments to prevent holding onto stale connections dropped by the server.
		logger.Infof("[DB] Turso detected. Setting MaxIdleConns to 0, MaxOpenConns to 20.")
		idleConns = 0
	} else {
		logger.Infof("[DB] SQLite (Local) detected. Setting MaxIdleConns to 2, MaxOpenConns to 10.")
	}
	db.SetMaxIdleConns(idleConns)
	db.SetMaxOpenConns(20)
	db.SetConnMaxLifetime(1 * time.Minute)
	if idleConns > 0 {
		db.SetConnMaxIdleTime(30 * time.Second)
	}
}

func GetDB() *sql.DB {
	return db
}

// RunInTx executes a database transaction and automatically rolls it back if an error occurs.
//Why: Enforces consistent transaction management across the gamification domain to ensure data integrity.
func RunInTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

// LogDBStats outputs current connection pool statistics for monitoring.
//Why: Enables observability of database performance and connection usage in production environments.
func LogDBStats() {
	if db == nil {
		return
	}
	stats := db.Stats()
	logger.Debugf("[DB-STATS] Open: %d | InUse: %d | Idle: %d | WaitCount: %d", stats.OpenConnections, stats.InUse, stats.Idle, stats.WaitCount)
}
