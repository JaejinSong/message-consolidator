package store

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/config"
	"message-consolidator/logger"
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
	conn     *sql.DB
	dsn      string
	testMode bool
)

func InitDB(ctx context.Context, cfg *config.Config) error {
	var err error
	driverName, finalURL := GetDBDriverAndDSN(cfg)

	// Why: Reuse healthy connection if DSN matches (idempotent re-init after ResetForTest).
	if conn != nil && dsn == finalURL {
		if err := conn.Ping(); err == nil {
			return EnsureSchemaAndSeeds(conn)
		}
	}

	// Close stale connection if DSN changed or ping failed.
	if conn != nil {
		conn.Close()
		conn = nil
	}

	dsn = finalURL
	conn, err = sql.Open(driverName, finalURL)
	if err != nil {
		return fmt.Errorf("failed to open database (%s): %w", driverName, err)
	}

	setupConnectionPool(cfg, finalURL)
	applySQLitePragmas(conn, finalURL)

	if strings.HasPrefix(finalURL, "libsql://") {
		// Why: Start background keep-alive for remote connections to prevent idle timeouts.
		go startKeepAlive(ctx, conn, cfg.DBKeepAliveInterval)
	}

	return EnsureSchemaAndSeeds(conn)
}

// GetDBDriverAndDSN constructs the appropriate driver name and DSN based on configuration.
func GetDBDriverAndDSN(cfg *config.Config) (string, string) {
	dbURL := cfg.TursoURL
	if dbURL == "" {
		// Why: Use a dedicated test.db file for the entire test suite.
		// Shared-memory SQLite is too volatile across connection pools in Go.
		dbURL = "file:test.db?_busy_timeout=10000"
	}
	if strings.HasPrefix(dbURL, "libsql://") && cfg.TursoToken != "" {
		dbURL = fmt.Sprintf("%s?authToken=%s", dbURL, cfg.TursoToken)
	}

	driverName := "sqlite"
	if strings.HasPrefix(dbURL, "libsql://") {
		driverName = "libsql"
	}
	return driverName, dbURL
}

// applySQLitePragmas enforces WAL mode and synchronous settings for local file-based databases.
func applySQLitePragmas(db *sql.DB, dbURL string) {
	if !strings.HasPrefix(dbURL, "file:") {
		return
	}
	if _, err := db.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		logger.Warnf("[DB-INIT] Failed to set WAL mode: %v", err)
	}
	if _, err := db.Exec("PRAGMA synchronous = NORMAL;"); err != nil {
		logger.Warnf("[DB-INIT] Failed to set synchronous=NORMAL: %v", err)
	}
}

// EnsureSchemaAndSeeds ensures that all core tables, migrations, and seed data are present.
// It is idempotent and safe to call multiple times.
func EnsureSchemaAndSeeds(dbConn *sql.DB) error {
	ctx := context.Background()
	// Why: Use LevelDefault for SQLite file-based DBs. Serializable causes SQLITE_BUSY
	// on DDL in WAL mode. The original Serializable was needed for in-memory shared-cache
	// connections (abandoned since modernc.org/sqlite doesn't support cache=shared).
	tx, err := dbConn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return fmt.Errorf("failed to start setup transaction: %w", err)
	}
	defer tx.Rollback()

	logger.Infof("[DB-INIT] Starting core table creation...")
	if err := createCoreTables(ctx, tx); err != nil {
		return fmt.Errorf("core table creation failed: %w", err)
	}
	logger.Infof("[DB-INIT] Core tables created/verified.")

	// Why: Perform schema migrations to add new columns to existing tables.
	logger.Infof("[DB-INIT] Starting migrations...")
	if err := runMigrations(ctx, tx); err != nil {
		return fmt.Errorf("database migration failed: %w", err)
	}
	logger.Infof("[DB-INIT] Migrations completed.")

	// Why: Rebuild views AFTER tables and columns exist to ensure they reference current schema.
	logger.Infof("[DB-INIT] Rebuilding views...")
	if err := rebuildViews(ctx, tx); err != nil {
		return fmt.Errorf("view rebuild failed: %w", err)
	}

	logger.Infof("[DB-INIT] Creating indexes...")
	createIndexes(ctx, tx)
	logger.Infof("[DB-INIT] Indexes created.")

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit setup transaction: %w", err)
	}
	logger.Infof("[DB-INIT] Schema committed successfully.")

	// Why: Skip expensive cache refresh during tests to maximize speed.
	// Tests will lazily initialize cache if needed via EnsureCacheInitialized.
	if testMode {
		return nil
	}

	return RefreshAllCaches(context.Background())
}

func setupConnectionPool(cfg *config.Config, dbURL string) {
	maxOpen := cfg.DBMaxOpenConns
	// Why: Auto-tuning for SQLite if not explicitly overridden via environment.
	if strings.HasPrefix(dbURL, "file:") && maxOpen == 25 {
		maxOpen = 100
	}

	conn.SetMaxOpenConns(maxOpen)
	conn.SetMaxIdleConns(cfg.DBMaxIdleConns) // Why: Prevents 'stream is closed' by maintaining a stable pool.
	// Why: Maintains existing 5-minute policy while preventing stale connection accumulation.
	conn.SetConnMaxLifetime(5 * time.Minute)
	conn.SetConnMaxIdleTime(30 * time.Second)
}

// startKeepAlive periodically pings the database to prevent the server or proxy from closing idle connections.
// Why: [Reliability] Maintains an active connection stream for remote Turso/libsql databases.
func startKeepAlive(ctx context.Context, db *sql.DB, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Infof("[DB-KEEPALIVE] Stopping background keep-alive.")
			return
		case <-ticker.C:
			handleKeepAliveTick(ctx, db)
		}
	}
}

// handleKeepAliveTick encapsulates the ping logic to maintain a maximum nesting depth of 2 in the worker loop.
func handleKeepAliveTick(ctx context.Context, db *sql.DB) {
	if db == nil {
		return
	}
	if err := db.PingContext(ctx); err != nil {
		logger.Warnf("[DB-KEEPALIVE] Periodic ping failed: %v", err)
	}
}

// LogSQLError provides unified logging for database errors with query context.
// Why: [Observability] Centralizes error reporting and ensures consistent context (query & args) in logs.
func LogSQLError(query string, err error, args ...interface{}) error {
	if err == nil {
		return nil
	}
	// Why: Detailed logging including query and arguments to accelerate remote debugging of SQL failures.
	logger.Errorf("[DB-ERROR] SQL_FAILED | Query: %s | Args: %v | Err: %v", query, args, err)
	return fmt.Errorf("database error in %s: %w", query, err)
}

func GetDB() *sql.DB {
	return conn
}

func GetDSN() string {
	return dsn
}

// RunInTx executes a database transaction and automatically rolls it back if an error occurs.
// Why: Enforces consistent transaction management across the gamification domain to ensure data integrity.
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
// Why: Enables observability of database performance and connection usage in production environments.
func LogDBStats() {
	if conn == nil {
		return
	}
	stats := conn.Stats()
	logger.Debugf("[DB-STATS] Open: %d | InUse: %d | Idle: %d | WaitCount: %d", stats.OpenConnections, stats.InUse, stats.Idle, stats.WaitCount)
}
