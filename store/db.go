package store

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/config"
	"message-consolidator/logger"
	"strings"
	"time"

	// Why: Registers the libsql SQL driver via init() so sql.Open("libsql", ...) resolves at runtime.
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	"github.com/whatap/go-api/instrumentation/database/sql/whatapsql"
	"github.com/whatap/go-api/trace"
	// Why: Registers modernc.org/sqlite as the "sqlite" driver for the in-memory test DB and local fallback paths.
	_ "modernc.org/sqlite"
)

// Querier is a common interface for sql.DB and sql.Tx.
// any 사유: database/sql 표준 시그니처와 일치 — driver가 query placeholder별 임의 타입 인자를 수용해야 함.
// 메서드 7개 사유: database/sql 표준 라이브러리가 sql.DB와 sql.Tx에 공통 supertype을 제공하지 않아 둘 다에 sqlc/raw 호출을 위임하려면 7개 메서드를 모두 노출해야 함. 분할 시 모든 호출처가 합쳐진 인터페이스를 다시 요구.
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

	// TestDSN allows tests to supply a custom DSN (set before InitDB is called).
	// Why: Forces a single shared in-memory connection for test isolation.
	TestDSN string
)

func InitDB(ctx context.Context, cfg *config.Config) error {
	var err error
	driverName, finalURL := GetDBDriverAndDSN(cfg)

	// Why: Reuse healthy connection if DSN matches (idempotent re-init after ResetForTest).
	if conn != nil && dsn == finalURL {
		if err := conn.Ping(); err == nil {
			return EnsureSchemaAndSeeds(ctx, conn)
		}
	}

	// Close stale connection if DSN changed or ping failed.
	if conn != nil {
		conn.Close()
		conn = nil
	}

	dsn = finalURL
	// Why: whatapsql.OpenContext wraps the underlying driver so every Exec/Query/QueryRow
	// becomes a WhaTap SQL trace step automatically. Falls back to plain sql.Open when
	// trace is disabled or `go_sql_profile_enabled=false`. sqlc's DBTX interface is
	// satisfied transparently — no generated code changes required.
	conn, err = whatapsql.OpenContext(ctx, driverName, finalURL)
	if err != nil {
		return fmt.Errorf("failed to open database (%s): %w", driverName, err)
	}

	setupConnectionPool(cfg, finalURL)
	applySQLitePragmas(conn, finalURL)

	if strings.HasPrefix(finalURL, "libsql://") {
		// Why: Start background reachability heartbeat. Note: with maxIdle=0 this does NOT
		// keep a warm pool conn — every user request opens its own fresh stream — but the
		// periodic ping surfaces Turso connectivity loss in logs/WhaTap before users hit it.
		logger.Infof("[DB-KEEPALIVE] Starting heartbeat (interval=%v)", cfg.DBKeepAliveInterval)
		go startKeepAlive(ctx, conn, cfg.DBKeepAliveInterval)
	}

	return EnsureSchemaAndSeeds(ctx, conn)
}

// GetDBDriverAndDSN constructs the appropriate driver name and DSN based on configuration.
func GetDBDriverAndDSN(cfg *config.Config) (string, string) {
	// Why: TestDSN takes priority — allows testutil to inject an in-memory DSN.
	if TestDSN != "" {
		return "sqlite", TestDSN
	}
	dbURL := cfg.TursoURL
	if dbURL == "" {
		// Why: In production dev mode with no TursoURL, use a local file.
		dbURL = "file:local.db?_pragma=busy_timeout(10000)"
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

// applySQLitePragmas enforces WAL mode, synchronous settings, and busy timeout for local file-based databases.
func applySQLitePragmas(db *sql.DB, dbURL string) {
	if !strings.HasPrefix(dbURL, "file:") {
		return
	}
	// Why: Belt-and-suspenders for busy_timeout. The _pragma DSN param sets it at open,
	// but we set it again here to guarantee it applies to all connections in the pool.
	if _, err := db.Exec("PRAGMA busy_timeout = 10000;"); err != nil {
		logger.Warnf("[DB-INIT] Failed to set busy_timeout: %v", err)
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
func EnsureSchemaAndSeeds(ctx context.Context, dbConn *sql.DB) error {
	// Why: Use LevelDefault for SQLite file-based DBs. Serializable causes SQLITE_BUSY
	// on DDL in WAL mode. The original Serializable was needed for in-memory shared-cache
	// connections (abandoned since modernc.org/sqlite doesn't support cache=shared).
	tx, err := dbConn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return fmt.Errorf("failed to start setup transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

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

	return RefreshAllCaches(ctx)
}

func setupConnectionPool(cfg *config.Config, dbURL string) {
	maxOpen := cfg.DBMaxOpenConns
	maxIdle := cfg.DBMaxIdleConns
	maxLifetime := 5 * time.Minute

	// Why: Auto-tuning for SQLite if not explicitly overridden via environment.
	if strings.HasPrefix(dbURL, "file:") && maxOpen == 25 {
		maxOpen = 100
	}

	// Why: If we are in test mode (specifically using in-memory SQLite), we MUST
	// maintain exactly one active connection to keep the in-memory DB alive.
	// modernc.org/sqlite ignores cache=shared, and closing the last connection
	// destroys the DB. maxIdle=1 ensures the connection stays in the pool.
	if testMode {
		maxOpen = 1
		maxIdle = 1
		maxLifetime = 1 * time.Hour // Prevent connection turnover during tests
	}

	// Why: libSQL HTTP streams expire after 10s server-side idle. Keeping idle pool
	// conns risks "stream is closed: bad connection" on next use (regressed when we
	// briefly tried maxIdle=1 + 6s ping — keepalive ping windows still leak past
	// timeout, e.g. mid-tx >10s operations or ticker GC pauses). hranaV2's
	// ResetSession also calls closeStream every borrow, so a "warm pool" only
	// reuses TCP+TLS, not the stream — the perf benefit is marginal.
	// maxIdle=0 forces every request to open a fresh stream — robust by design.
	if strings.HasPrefix(dbURL, "libsql://") {
		maxIdle = 0
	}

	conn.SetMaxOpenConns(maxOpen)
	conn.SetMaxIdleConns(maxIdle)
	conn.SetConnMaxLifetime(maxLifetime)
	if maxIdle > 0 {
		conn.SetConnMaxIdleTime(30 * time.Second)
	}
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
// Why: Each tick is wrapped as its own bounded WhaTap transaction (per CLAUDE.md WhaTap policy — `/`
// prefix so urlutil.NewURL parses it as Path, not Host).
func handleKeepAliveTick(ctx context.Context, db *sql.DB) {
	if db == nil {
		return
	}
	traceCtx, _ := trace.Start(ctx, "/Background-DBKeepAlive")
	err := db.PingContext(traceCtx)
	_ = trace.End(traceCtx, err)
	if err != nil {
		logger.Warnf("[DB-KEEPALIVE] Periodic ping failed: %v", err)
	}
}

// LogSQLError provides unified logging for database errors with query context.
// Why: [Observability] Centralizes error reporting and ensures consistent context (query & args) in logs.
// any 사유: SQL placeholder 인자는 query마다 타입이 다름 — Querier와 동일한 variadic any 채택.
func LogSQLError(query string, err error, args ...any) error {
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

// RunInTx executes a database transaction and automatically rolls it back if an error occurs.
// Why: Enforces consistent transaction management across the gamification domain to ensure data integrity.
func RunInTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return err
	}

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
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
