package handlers

import (
	"context"
	"message-consolidator/config"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestHandleManualScan_TriggersScanFunc(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	var called atomic.Int32
	done := make(chan struct{})
	prev := ScanFunc
	ScanFunc = func(email, lang string) {
		called.Add(1)
		if email != "u@example.com" || lang != "Korean" {
			t.Errorf("ScanFunc got email=%q lang=%q", email, lang)
		}
		close(done)
	}
	t.Cleanup(func() { ScanFunc = prev })

	api := &API{}
	req := NewMockRequest("POST", "/api/scan", "u@example.com")
	rr := httptest.NewRecorder()

	api.HandleManualScan(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ScanFunc was not invoked within 2s")
	}
	if got := called.Load(); got != 1 {
		t.Errorf("expected 1 invocation, got %d", got)
	}
}

func TestHandleInternalScan_AuthRules(t *testing.T) {
	tests := []struct {
		name        string
		secret      string
		header      string
		wantStatus  int
		wantInvoked bool
	}{
		{"unconfigured secret returns 403", "", "anything", http.StatusForbidden, false},
		{"wrong header returns 401", "right", "wrong", http.StatusUnauthorized, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var invoked atomic.Bool
			prev := FullScanFunc
			FullScanFunc = func() { invoked.Store(true) }
			t.Cleanup(func() { FullScanFunc = prev })

			api := &API{Config: &config.Config{InternalScanSecret: tt.secret}}
			req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/internal/scan", nil)
			if tt.header != "" {
				req.Header.Set("X-Internal-Secret", tt.header)
			}
			rr := httptest.NewRecorder()

			api.HandleInternalScan(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status: got %d want %d", rr.Code, tt.wantStatus)
			}
			if invoked.Load() != tt.wantInvoked {
				t.Errorf("FullScanFunc invoked=%v want=%v", invoked.Load(), tt.wantInvoked)
			}
		})
	}
}
