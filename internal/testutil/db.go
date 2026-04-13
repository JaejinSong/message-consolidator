package testutil

import (
	"fmt"
	"message-consolidator/config"
	"os"
	"time"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

// SetupTestDB initializes a unified SQLite database for testing at ./test.db.
// It returns a cleanup function and requires an initFunc to avoid import cycles.
func SetupTestDB(initFunc func(*config.Config) error, resetFunc func()) (func(), error) {
	if resetFunc != nil {
		resetFunc()
	}

	// Why: Use a single shared in-memory DB for the entire test suite.
	// We handle isolation by running extremely fast TRUNCATE/DELETE in a transaction between tests.
	dbURL := "file:memdb_shared?mode=memory&cache=shared&_busy_timeout=10000"

	cfg := &config.Config{
		TursoURL: dbURL,
	}

	if err := initFunc(cfg); err != nil {
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
func RemoveTestDBFiles(path string) {
	_ = os.Remove(path)
	_ = os.Remove(path + "-shm")
	_ = os.Remove(path + "-wal")
	_ = os.Remove(path + "-journal")
}

// RandomEmail generates a unique email address for test data isolation.
// Why: Ensures parallel tests sharing a single test.db never clash on unique constraints.
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
