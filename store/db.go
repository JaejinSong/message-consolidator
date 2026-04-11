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

// Querier is a common interface for sql.DB and sql.Tx
type Querier interface {
	Exec(query string, args ...any) (sql.Result, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

var (
	db  *sql.DB
	dsn string
)

func InitDB(cfg *config.Config) error {
	//Why: Ensures InitDB is idempotent across the lifetime of the application process.
	//This is critical for unit tests sharing a single process, preventing redundant pool initialization and schema migration.
	if db != nil {
		if err := db.Ping(); err == nil {
			return nil
		}
		// If ping fails, we attempt to re-open
		logger.Warnf("[DB] Existing connection check failed. Re-initializing...")
	}

	var err error
	dbURL := cfg.TursoURL
	authToken := cfg.TursoToken

	//Why: Enforces a consistent local development database path (db/test.db) if no remote Turso URL is provided,
	//preventing the creation of "mystery" database files in random directories.
	if dbURL == "" && !cfg.CloudRunMode {
		dbURL = "file:db/test.db?cache=shared&_busy_timeout=30000"
		logger.Warnf("[DB] TURSO_DATABASE_URL is empty. Defaulting to local unified DB: %s", dbURL)
	}

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
	logger.Infof("[DB] Initializing database connection: %s", maskedURL)
	if strings.Contains(dbURL, "test.db") {
		logger.Infof("[DB-TEST] Test database detected: ./test.db unified mapping active.")
	}

	dsn = dbURL
	// Optimization: Inject _txlock=immediate for all local SQLite connections to resolve parallel test deadlocks.
	if strings.HasPrefix(dsn, "file:") {
		if u, err := url.Parse(dsn); err == nil {
			q := u.Query()
			if q.Get("_txlock") == "" {
				q.Set("_txlock", "immediate")
				u.RawQuery = q.Encode()
				dsn = u.String()
			}
		}
	}

	dbURL = dsn
	driverName := "sqlite"
	if strings.HasPrefix(dbURL, "libsql://") {
		driverName = "libsql"
	}
	db, err = sql.Open(driverName, dbURL)
	if err != nil {
		return fmt.Errorf("failed to open database (%s): %w", driverName, err)
	}

	setupConnectionPool(dbURL)
	// Extra safety: Explicitly set busy_timeout even if it's in DSN.
	_, _ = db.Exec("PRAGMA busy_timeout = 30000;")

	// Why: If it's local SQLite, wrap initialization in a write transaction to prevent concurrent processes from clashing on schema changes.
	var tx *sql.Tx
	if strings.HasPrefix(dbURL, "file:") {
		// Attempt to start a write lock immediately.
		tx, err = db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelDefault})
		if err == nil {
			_, _ = tx.Exec("BEGIN IMMEDIATE;")
		}
	}

	// Helper to exec on tx if exists, else db.
	var q Querier = db
	if tx != nil {
		q = tx
	}

	// Why: Ensure core tables exist BEFORE running migrations. 
	// This allows migrations (like ALTER TABLE) to target existing tables safely.
	if err := createCoreTables(q); err != nil {
		if tx != nil { tx.Rollback() }
		return fmt.Errorf("core table creation failed: %w", err)
	}

	// Why: Perform schema migrations to add new columns to existing tables.
	// We now treat migration failures as fatal to prevent "no such column" errors later.
	if err := runMigrations(q); err != nil {
		if tx != nil { tx.Rollback() }
		return fmt.Errorf("database migration failed: %w", err)
	}

	if err := setupGamification(q); err != nil {
		if tx != nil { tx.Rollback() }
		return fmt.Errorf("gamification setup failed: %w", err)
	}

	createIndexes(q)

	if tx != nil {
		if err := tx.Commit(); err != nil {
			logger.Warnf("[DB-INIT] Failed to commit initialization transaction: %v", err)
		}
	}

	return RefreshAllCaches(context.Background())
}

func setupConnectionPool(connStr string) {
	//Why: Configures connection pool settings to prevent "stream is closed" or "bad connection" errors in serverless environments like Turso.
	maxOpen := 20
	idleConns := 2
	if strings.HasPrefix(connStr, "libsql://") {
		//Why: Disables idle connections for remote Turso environments to prevent holding onto stale connections dropped by the server.
		logger.Infof("[DB] Turso detected. Setting MaxIdleConns to 0, MaxOpenConns to 20.")
		idleConns = 0
	} else {
		//Why: For local SQLite tests running sequentially, MaxOpenConns=1 is the only robust way to prevent `database is locked (5)` 
		//errors caused by connection pool starvation or PRAGMA mismatches across multiple connections.
		logger.Infof("[DB] SQLite (Local) detected. Setting MaxIdleConns to 1, MaxOpenConns to 1 (WAL mode).")
		maxOpen = 1
		idleConns = 1

		//Why: Check current state to avoid unnecessary lock requests if WAL is already active.
		var currentMode string
		_ = db.QueryRow("PRAGMA journal_mode;").Scan(&currentMode)
		if strings.ToLower(currentMode) != "wal" {
			if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
				logger.Warnf("[DB] Failed to enable WAL mode: %v", err)
			}
		}

		var currentSync int
		_ = db.QueryRow("PRAGMA synchronous;").Scan(&currentSync)
		if currentSync != 1 { // 1 = NORMAL
			if _, err := db.Exec("PRAGMA synchronous=NORMAL;"); err != nil {
				logger.Warnf("[DB] Failed to set PRAGMA synchronous=NORMAL: %v", err)
			}
		}
	}
	db.SetMaxIdleConns(idleConns)
	db.SetMaxOpenConns(maxOpen)
	db.SetConnMaxLifetime(1 * time.Minute)
	if idleConns > 0 {
		db.SetConnMaxIdleTime(30 * time.Second)
	}
}

func GetDB() *sql.DB {
	return db
}

func GetDSN() string {
	if !strings.HasPrefix(dsn, "file:") {
		return dsn
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	q := u.Query()
	if q.Get("_txlock") == "" {
		q.Set("_txlock", "immediate")
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// RunInTx executes a database transaction and automatically rolls it back if an error occurs.
//Why: Enforces consistent transaction management across the gamification domain to ensure data integrity.
func RunInTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
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
