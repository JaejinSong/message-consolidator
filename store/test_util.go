package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

// SetupTestDB initializes a temporary SQLite database for testing.
// It returns a cleanup function to remove the temporary file.
func SetupTestDB() (func(), error) {
	ResetForTest()

	tempDir := os.TempDir()
	dbPath := filepath.Join(tempDir, fmt.Sprintf("test_db_%d.sqlite", os.Getpid()))
	dbURL := "file:" + dbPath

	var err error
	db, err = sql.Open("libsql", dbURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open test database: %w", err)
	}

	if err := createCoreTables(); err != nil {
		db.Close()
		os.Remove(dbPath)
		return nil, fmt.Errorf("failed to create core tables: %w", err)
	}

	if err := runMigrations(); err != nil {
		db.Close()
		os.Remove(dbPath)
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	if err := setupGamification(); err != nil {
		db.Close()
		os.Remove(dbPath)
		return nil, fmt.Errorf("failed to setup gamification: %w", err)
	}

	createIndexes()

	cleanup := func() {
		db.Close()
		os.Remove(dbPath)
	}

	return cleanup, nil
}
