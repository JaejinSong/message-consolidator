package channels

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"message-consolidator/config"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"message-consolidator/types"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

func newStubGmailService(t *testing.T, handler http.Handler) *gmail.Service {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	svc, err := gmail.NewService(context.Background(), option.WithoutAuthentication(), option.WithEndpoint(srv.URL))
	if err != nil {
		t.Fatalf("failed to create stub gmail service: %v", err)
	}
	return svc
}

func stubFullMessageJSON(id string, ts int64) string {
	body := base64.RawURLEncoding.EncodeToString([]byte("Please review the staging report."))
	return fmt.Sprintf(`{
		"id": %q, "threadId": "t-%s", "internalDate": "%d000", "labelIds": ["INBOX"],
		"payload": {
			"mimeType": "text/plain",
			"headers": [
				{"name": "Subject", "value": "hello"},
				{"name": "From", "value": "Alice <alice@example.com>"},
				{"name": "To", "value": "test@example.com"}
			],
			"body": {"data": %q}
		}
	}`, id, id, ts, body)
}

func TestParseNewEmailsGetErrorHoldsCursor(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test db: %v", err)
	}
	defer cleanup()

	const okTS = int64(1783500000)
	svc := newStubGmailService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/messages/m-broken") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(stubFullMessageJSON("m-ok", okTS)))
	}))

	msgs := []*gmail.Message{{Id: "m-ok"}, {Id: "m-broken"}}
	rawMsgs, _, _, maxTS, ok := parseNewEmails(context.Background(), svc, "test@example.com", msgs, &config.Config{})

	//Why: A failed Get means the message was neither analyzed nor marked processed —
	// the caller must hold the scan cursor or the message is skipped forever.
	if ok {
		t.Error("ok should be false when any message Get fails")
	}
	if len(rawMsgs) != 1 || rawMsgs[0].ID != "m-ok" {
		t.Errorf("expected only m-ok parsed, got %+v", rawMsgs)
	}
	if maxTS != okTS {
		t.Errorf("maxTS = %d, want %d", maxTS, okTS)
	}
}

func TestParseNewEmailsAllSuccessAdvancesCursor(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test db: %v", err)
	}
	defer cleanup()

	const okTS = int64(1783500000)
	svc := newStubGmailService(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(stubFullMessageJSON("m-ok", okTS)))
	}))

	_, _, _, maxTS, ok := parseNewEmails(context.Background(), svc, "test@example.com", []*gmail.Message{{Id: "m-ok"}}, &config.Config{})

	if !ok {
		t.Error("ok should be true when every message is fetched")
	}
	if maxTS != okTS {
		t.Errorf("maxTS = %d, want %d", maxTS, okTS)
	}
}

func TestAnalyzeAndSaveEmailsMissingDepsHoldsCursor(t *testing.T) {
	//Why: scanner.Init failure must not let the cursor advance past unanalyzed mail.
	ids, ok := analyzeAndSaveEmails(context.Background(), "test@example.com", "Korean",
		[]types.RawMessage{{ID: "m1"}}, nil, nil, nil, nil, nil)
	if ok {
		t.Error("ok should be false when gc/filterSvc are missing")
	}
	if ids != nil {
		t.Errorf("expected no ids, got %v", ids)
	}
}
