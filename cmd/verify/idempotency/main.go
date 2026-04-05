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
	// 1. Setup Test DB
	cfg := &config.Config{
		TursoURL: "file:idempotency_test.db",
	}
	store.InitDB(cfg)
	defer os.Remove("idempotency_test.db")

	email := "test@example.com"
	
	// 2. Test User Idempotency
	log.Println("[CHECK] User Idempotency...")
	u1, _ := store.GetOrCreateUser(context.Background(), email, "Name 1", "Pic 1")
	u2, _ := store.GetOrCreateUser(context.Background(), email, "Name 2", "Pic 2")
	
	if u1.ID != u2.ID {
		log.Fatalf("FAIL: User ID mismatch after upsert: %d != %d", u1.ID, u2.ID)
	}
	if u2.Name != "Name 2" {
		log.Fatalf("FAIL: User name not updated: %s", u2.Name)
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
		log.Fatalf("FAIL: Initial save failed: %v", err)
	}
	msgID := ids[0]
	
	// Duplicate SourceTS with different task should trigger Upsert
	msg.Task = "Updated Task"
	ids2, err := store.SaveMessages(context.Background(), []store.ConsolidatedMessage{msg})
	if err != nil || len(ids2) == 0 {
		log.Fatalf("FAIL: Upsert save failed: %v", err)
	}
	
	if ids2[0] != msgID {
		log.Fatalf("FAIL: Upsert returned different ID: %d != %d", ids2[0], msgID)
	}
	
	m, _ := store.GetMessageByID(context.Background(), email, msgID)
	if m.Task != "Updated Task" {
		log.Fatalf("FAIL: Message not updated (Upsert failed): got '%s', want 'Updated Task'", m.Task)
	}
	log.Println("SUCCESS: Message Idempotency (Upsert) verified.")

	// 4. Test Achievement Idempotency
	log.Println("[CHECK] Achievement Idempotency...")
	_ = store.UnlockAchievement(context.Background(), int(u1.ID), 1)
	_ = store.UnlockAchievement(context.Background(), int(u1.ID), 1) // Second unlock
	
	achs, _ := store.GetUserAchievements(context.Background(), int(u1.ID))
	if len(achs) != 1 {
		log.Fatalf("FAIL: Achievement duplicated: found %d", len(achs))
	}
	log.Println("SUCCESS: Achievement Idempotency verified.")

	fmt.Println("\n[FINAL RESULT] ALL IDEMPOTENCY CHECKS PASSED.")
}
