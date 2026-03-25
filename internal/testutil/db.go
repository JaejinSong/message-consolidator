package testutil

import (
	"fmt"
	"os"
	"path/filepath"

	"message-consolidator/config"
	"message-consolidator/store"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

// SetupTestDB initializes a temporary SQLite database for testing.
// It returns a cleanup function to remove the temporary file.
// It enforces the BP rule of separating test utilities from production code.
func SetupTestDB() (func(), error) {
	store.ResetForTest()

	tempDir := os.TempDir()
	dbPath := filepath.Join(tempDir, fmt.Sprintf("test_db_%d.sqlite", os.Getpid()))
	dbURL := "file:" + dbPath

	cfg := &config.Config{
		TursoURL: dbURL,
	}

	if err := store.InitDB(cfg); err != nil {
		os.Remove(dbPath)
		return nil, fmt.Errorf("failed to init test database: %w", err)
	}

	cleanup := func() {
		if db := store.GetDB(); db != nil {
			db.Close()
		}
		os.Remove(dbPath)
	}

	return cleanup, nil
}
