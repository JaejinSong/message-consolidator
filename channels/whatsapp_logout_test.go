package channels

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/types/events"
)

// Why: a server-side unlink deletes the device store; at Debug level it was invisible in prod
// (LOG_LEVEL=INFO) and WhatsApp ingestion stopped silently for a week (2026-10-12 incident).
func TestHandleEvent_LoggedOutLogsWarnWithReason(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	m := NewWAManager()
	var loggedOut string
	m.OnLoggedOut = func(email string) { loggedOut = email }

	m.handleEvent("user@example.com", nil, &events.LoggedOut{OnConnect: true, Reason: events.ConnectFailureLoggedOut})

	out := buf.String()
	if !strings.Contains(out, "[WARN]") || !strings.Contains(out, "user@example.com") {
		t.Fatalf("expected WARN log naming the user, got %q", out)
	}
	if !strings.Contains(out, events.ConnectFailureLoggedOut.String()) {
		t.Errorf("expected logout reason in log, got %q", out)
	}
	if loggedOut != "user@example.com" {
		t.Errorf("OnLoggedOut called with %q, want user@example.com", loggedOut)
	}
}
