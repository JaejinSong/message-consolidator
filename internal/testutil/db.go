package testutil

import (
	"fmt"
	"message-consolidator/config"
	"os/exec"
	"strings"
	"time"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

// SetupTestDB initializes a unified SQLite database for testing at ./test.db.
// It returns a cleanup function and requires an initFunc to avoid import cycles.
func SetupTestDB(initFunc func(*config.Config) error, resetFunc func()) (func(), error) {
	if resetFunc != nil {
		resetFunc()
	}

	// Why: Point to exactly one test.db at the project root to satisfy the "unification" goal.
	// We resolve the absolute path to the project root to prevent multi-file creation in subpackages.
	root, _ := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	rootPath := strings.TrimSpace(string(root))
	if rootPath == "" {
		rootPath = "."
	}
	dbURL := fmt.Sprintf("file:%s/test.db?cache=shared&_busy_timeout=30000", rootPath)

	cfg := &config.Config{
		TursoURL: dbURL,
	}

	if err := initFunc(cfg); err != nil {
		return nil, fmt.Errorf("failed to init test database: %w", err)
	}

	cleanup := func() {
		// Why: Standard cleanup only resets in-memory caches.
		// Physical file removal is excluded to allow developer inspection.
		if resetFunc != nil {
			resetFunc()
		}
	}

	return cleanup, nil
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
