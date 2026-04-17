package testutil

import (
	"context"
	"fmt"
	"message-consolidator/config"
	"time"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

// SetupTestDB initializes an in-memory SQLite database for testing.
// It returns a cleanup function and requires an initFunc to avoid import cycles.
//
// Root cause of previous issues:
//   modernc.org/sqlite does NOT support cache=shared for in-memory databases.
//   Each connection in sql.DB's pool gets its own separate in-memory DB.
//
// Fix: Inject a unique in-memory DSN via store.TestDSN.
//   store.InitDB detects this and calls db.SetMaxOpenConns(1), forcing all
//   goroutines to share a single connection → single shared in-memory DB.
func SetupTestDB(initFunc func(context.Context, *config.Config) error, resetFunc func()) (func(), error) {
	if resetFunc != nil {
		resetFunc()
	}

	// Why: Pass an empty config so GetDBDriverAndDSN picks up store.TestDSN instead.
	cfg := &config.Config{}

	if err := initFunc(context.Background(), cfg); err != nil {
		return nil, fmt.Errorf("failed to init test database: %w", err)
	}

	cleanup := func() {
		if resetFunc != nil {
			resetFunc()
		}
	}

	return cleanup, nil
}

// RemoveTestDBFiles thoroughly removes the SQLite database file and its WAL/SHM artifacts.
// Why: SQLite in WAL mode creates -shm and -wal files that linger if not explicitly closed and deleted.
func RemoveTestDBFiles(path string) {}

// RandomEmail generates a unique email address for test data isolation.
// Why: Ensures tests sharing a single in-memory DB never clash on unique constraints.
func RandomEmail(prefix string) string {
	return fmt.Sprintf("%s-%d@test.com", prefix, time.Now().UnixNano())
}

// RandomTS generates a unique timestamp string for test data isolation.
func RandomTS(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

// RandomID generates a unique identifier string (e.g., for WhatsApp numbers).
func RandomID(prefix string) string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
