package main

import (
	"context"
	"fmt"
	"log"
	"message-consolidator/config"
	"message-consolidator/store"
	"os"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

func main() {
	// 1. Setup Verify DB (Shared Memory)
	dbURL := "file:memdb_verify?mode=memory&cache=shared"
	cfg := &config.Config{
		TursoURL: dbURL,
	}

	if err := store.InitDB(context.Background(), cfg); err != nil {
		log.Fatal(err)
	}

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Check failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	email := "test@example.com"

	log.Println("[CHECK] User Idempotency...")
	u1, _ := store.GetOrCreateUser(context.Background(), email, "Name 1", "Pic 1")
	u2, _ := store.GetOrCreateUser(context.Background(), email, "Name 2", "Pic 2")

	if u1.ID != u2.ID {
		return fmt.Errorf("User ID mismatch after upsert: %d != %d", u1.ID, u2.ID)
	}
	if u2.Name != "Name 2" {
		return fmt.Errorf("User name not updated: %s", u2.Name)
	}
	log.Println("SUCCESS: User Idempotency verified.")

	fmt.Println("\n[FINAL RESULT] ALL IDEMPOTENCY CHECKS PASSED.")
	return nil
}
