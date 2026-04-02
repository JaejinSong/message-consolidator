package testutil

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"message-consolidator/config"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

// SetupTestDB initializes a temporary SQLite database for testing.
// It returns a cleanup function and requires an initFunc to avoid import cycles.
func SetupTestDB(initFunc func(*config.Config) error, resetFunc func()) (func(), error) {
	if resetFunc != nil {
		resetFunc()
	}

	// Why: Use a unique in-memory SQLite database name per test to ensure isolation 
	// even when running multiple tests in parallel within the same process.
	b := make([]byte, 8)
	rand.Read(b)
	dbID := hex.EncodeToString(b)
	dbURL := fmt.Sprintf("file:%s?mode=memory&cache=shared", dbID)

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
