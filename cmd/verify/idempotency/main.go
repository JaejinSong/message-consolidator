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
	
	// 2. Test User Idempotency
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

	// 3. Test Message Idempotency
	log.Println("[CHECK] Message Idempotency...")
	msg := store.ConsolidatedMessage{
		UserEmail:    email,
		Room:         "Room",
		Task:         "Initial Task",
		Source:       "slack",
		SourceTS:     "1234567890.123456",
		OriginalText: "Original",
	}
	
	ids, err := store.SaveMessages(context.Background(), []store.ConsolidatedMessage{msg})
	if err != nil || len(ids) == 0 {
		return fmt.Errorf("initial save failed: %v", err)
	}
	msgID := ids[0]
	
	// Duplicate SourceTS with different task should trigger Upsert
	msg.Task = "Updated Task"
	ids2, err := store.SaveMessages(context.Background(), []store.ConsolidatedMessage{msg})
	if err != nil || len(ids2) == 0 {
		return fmt.Errorf("upsert save failed: %v", err)
	}
	
	if ids2[0] != msgID {
		return fmt.Errorf("upsert returned different ID: %d != %d", ids2[0], msgID)
	}
	
	m, _ := store.GetMessageByID(context.Background(), store.GetDB(), email, msgID)
	if m.Task != "Updated Task" {
		return fmt.Errorf("message not updated (Upsert failed): got '%s', want 'Updated Task'", m.Task)
	}
	log.Println("SUCCESS: Message Idempotency (Upsert) verified.")

	fmt.Println("\n[FINAL RESULT] ALL IDEMPOTENCY CHECKS PASSED.")
	return nil
}
