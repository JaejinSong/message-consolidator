package channels

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"message-consolidator/internal/testutil"
	"message-consolidator/store"

	"golang.org/x/oauth2"
)

func TestIsInvalidGrant(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"plain network error", errors.New("dial tcp: i/o timeout"), false},
		{"retrieve error invalid_grant", &oauth2.RetrieveError{ErrorCode: "invalid_grant"}, true},
		{"retrieve error other code", &oauth2.RetrieveError{ErrorCode: "invalid_client"}, false},
		{"retrieve error without code (non-JSON body)", &oauth2.RetrieveError{}, false},
		{"wrapped invalid_grant", fmt.Errorf("refresh: %w", &oauth2.RetrieveError{ErrorCode: "invalid_grant"}), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isInvalidGrant(tt.err); got != tt.want {
				t.Errorf("isInvalidGrant() = %v, want %v", got, tt.want)
			}
		})
	}
}

// withFakeTokenEndpoint points GmailOAuthConfig at a stub token server and restores it on cleanup.
func withFakeTokenEndpoint(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	orig := GmailOAuthConfig
	GmailOAuthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint:     oauth2.Endpoint{TokenURL: srv.URL},
	}
	t.Cleanup(func() {
		GmailOAuthConfig = orig
		srv.Close()
	})
}

func expiredTokenJSON() string {
	return fmt.Sprintf(`{"access_token":"expired","refresh_token":"dead","token_type":"Bearer","expiry":%q}`,
		time.Now().Add(-time.Hour).Format(time.RFC3339))
}

func TestGetGmailServiceInvalidGrantClearsToken(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test db: %v", err)
	}
	defer cleanup()

	withFakeTokenEndpoint(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Bad Request"}`))
	})

	email := "invalid-grant@example.com"
	if err := store.SaveGmailToken(context.Background(), email, expiredTokenJSON()); err != nil {
		t.Fatalf("failed to save token: %v", err)
	}

	if _, err := GetGmailService(context.Background(), email); err == nil {
		t.Fatal("expected refresh error, got nil")
	}
	//Why: The dead token must be purged so HasGmailToken and /gmail/status report disconnected.
	if store.HasGmailToken(email) {
		t.Error("token should be cleared after invalid_grant")
	}
}

func TestGetGmailServiceTransientErrorKeepsToken(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test db: %v", err)
	}
	defer cleanup()

	withFakeTokenEndpoint(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	email := "transient@example.com"
	if err := store.SaveGmailToken(context.Background(), email, expiredTokenJSON()); err != nil {
		t.Fatalf("failed to save token: %v", err)
	}

	if _, err := GetGmailService(context.Background(), email); err == nil {
		t.Fatal("expected refresh error, got nil")
	}
	//Why: A 5xx from the token endpoint is not a token-death verdict — clearing here would force needless re-auth.
	if !store.HasGmailToken(email) {
		t.Error("token must survive transient refresh failure")
	}
}
