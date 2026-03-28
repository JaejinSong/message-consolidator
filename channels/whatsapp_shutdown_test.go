package channels

import (
	"message-consolidator/logger"
	"testing"
	"time"
)

//Why: Ensures the graceful shutdown procedure for WhatsApp clients completes within the strict 2-second timeout bound, preventing application hanging during Docker SIGTERM signals.
func TestDisconnectAllWhatsApp_GracefulTimeout(t *testing.T) {
	// Initialize logger to prevent nil pointer errors
	logger.InitLogging()

	done := make(chan bool)

	startTime := time.Now()

	go func() {
		// Even with no clients, or if clients hang, this must return quickly.
		DisconnectAllWhatsApp()
		done <- true
	}()

	select {
	case <-done:
		duration := time.Since(startTime)
		if duration > 3*time.Second { // 2s max timeout + some buffer
			t.Fatalf("DisconnectAllWhatsApp took too long: %v, expected < 3s", duration)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DisconnectAllWhatsApp completely deadlocked, ignoring its 2s internal timeout.")
	}
}
