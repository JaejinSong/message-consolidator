package store

import (
	"fmt"
	"os"
	"path/filepath"

	"message-consolidator/config"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

func SetupTestDB() (func(), error) {
	ResetForTest()

	tempDir := os.TempDir()
	dbPath := filepath.Join(tempDir, fmt.Sprintf("test_db_%d.sqlite", os.Getpid()))
	dbURL := "file:" + dbPath + "?_busy_timeout=5000"

	cfg := &config.Config{
		TursoURL: dbURL,
	}

	if err := InitDB(cfg); err != nil {
		os.Remove(dbPath)
		return nil, fmt.Errorf("failed to init test database: %w", err)
	}

	cleanup := func() {
		if db != nil {
			db.Close()
		}
		os.Remove(dbPath)
	}

	return cleanup, nil
}
