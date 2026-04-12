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
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
}

var (
	conn *sql.DB
	dsn  string
)

func InitDB(cfg *config.Config) error {
	//Why: Ensures InitDB is idempotent across the lifetime of the application process.
	if conn != nil {
		if err := conn.Ping(); err == nil {
			return nil
		}
		logger.Warnf("[DB] Existing connection check failed. Re-initializing...")
	}

	var err error
	dbURL := cfg.TursoURL
	authToken := cfg.TursoToken

	if dbURL == "" && !cfg.CloudRunMode {
		dbURL = "file:test.db"
	}

	if strings.HasPrefix(dbURL, "libsql://") && authToken != "" {
		dbURL = fmt.Sprintf("%s?authToken=%s", dbURL, authToken)
	}

	dsn = dbURL
	dbURL = GetDSN()

	driverName := "sqlite"
	if strings.HasPrefix(dbURL, "libsql://") {
		driverName = "libsql"
	}

	conn, err = sql.Open(driverName, dbURL)
	if err != nil {
		return fmt.Errorf("failed to open database (%s): %w", driverName, err)
	}

	setupConnectionPool(dbURL)

	// Why: If it's local SQLite, enforce PRAGMAs for performance and concurrency.
	if strings.HasPrefix(dbURL, "file:") {
		// Enforce WAL mode and Synchronous=NORMAL for robust concurrent test execution.
		// These are executed outside a transaction to avoid connection pool deadlock when MaxOpenConns=1.
		if _, err := conn.Exec("PRAGMA journal_mode = WAL;"); err != nil {
			logger.Warnf("[DB-INIT] Failed to set WAL mode: %v", err)
		}
		if _, err := conn.Exec("PRAGMA synchronous = NORMAL;"); err != nil {
			logger.Warnf("[DB-INIT] Failed to set synchronous=NORMAL: %v", err)
		}
	}

	// Why: Ensure core tables exist BEFORE running migrations. 
	if err := createCoreTables(conn); err != nil {
		return fmt.Errorf("core table creation failed: %w", err)
	}

	// Why: Perform schema migrations to add new columns to existing tables.
	if err := runMigrations(conn); err != nil {
		return fmt.Errorf("database migration failed: %w", err)
	}

	if err := setupGamification(conn); err != nil {
		return fmt.Errorf("gamification setup failed: %w", err)
	}

	createIndexes(conn)

	return RefreshAllCaches(context.Background())
}

func setupConnectionPool(dbURL string) {
	var maxOpen, idleConns int
	//Why: Enforces strict connection limits to maintain SQLite stability and prevent resource exhaustion.
	// For local SQLite, we use a single connection to eliminate lock contention during concurrent test execution.
	if strings.HasPrefix(dbURL, "file:") {
		logger.Infof("[DB] SQLite (Local) detected. Enforcing MaxOpenConns=1 for 100%% lock safety.")
		maxOpen = 1
		idleConns = 1
	} else {
		logger.Infof("[DB] Turso (Remote) detected. Optimizing pool for high throughput.")
		maxOpen = 25
		idleConns = 10
	}

	conn.SetMaxOpenConns(maxOpen)
	conn.SetMaxIdleConns(idleConns)
	conn.SetConnMaxLifetime(10 * time.Minute)
	conn.SetConnMaxIdleTime(5 * time.Minute)
}

func GetDB() *sql.DB {
	return conn
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
	// Why: Remove cache=shared as it is incompatible with WAL mode and can cause deadlocks when MaxOpenConns=1.
	q.Del("cache")
	
	// Why: Ensure robust local concurrency by enforcing WAL mode, immediate write locks, and a long busy timeout for the connection pool.
	q.Set("_txlock", "immediate")
	q.Set("_journal_mode", "WAL")
	q.Set("_busy_timeout", "10000")
	
	u.RawQuery = q.Encode()
	return u.String()
}

// RunInTx executes a database transaction and automatically rolls it back if an error occurs.
//Why: Enforces consistent transaction management across the gamification domain to ensure data integrity.
func RunInTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
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
	if conn == nil {
		return
	}
	stats := conn.Stats()
	logger.Debugf("[DB-STATS] Open: %d | InUse: %d | Idle: %d | WaitCount: %d", stats.OpenConnections, stats.InUse, stats.Idle, stats.WaitCount)
}
