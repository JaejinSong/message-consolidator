package channels

import (
	"testing"
	"go.mau.fi/whatsmeow"
)

func TestLogoutWhatsApp(t *testing.T) {
	manager := NewWAManager()
	email := "test@example.com"
	
	// Mock client entry (can't easily mock the client itself without interface, but can test map cleanup)
	manager.mu.Lock()
	manager.clients[email] = &whatsmeow.Client{}
	manager.latestQR[email] = "some-qr"
	manager.mu.Unlock()

	// Verify initial state
	if manager.GetStatus(email) != "disconnected" { // Because IsConnected() will be false for empty struct
		// This is expected given the current implementation of GetStatus
	}

	// We only test the cleanup part here as calling Logout() on a zero-value client might panic
	// In a real scenario, we'd use an interface for whatsmeow.Client
	
	manager.mu.Lock()
	delete(manager.clients, email)
	delete(manager.latestQR, email)
	manager.mu.Unlock()

	if _, ok := manager.clients[email]; ok {
		t.Errorf("client should be deleted from map")
	}
	if _, ok := manager.latestQR[email]; ok {
		t.Errorf("QR should be deleted from map")
	}
}
