package handlers

import (
	"encoding/json"
	"message-consolidator/config"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleSlackStatus(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{"connected when token set", "xoxb-fake", "CONNECTED"},
		{"disconnected when token empty", "", "DISCONNECTED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &API{Config: &config.Config{SlackToken: tt.token}}
			req := httptest.NewRequest("GET", "/api/channels/slack/status", nil)
			rr := httptest.NewRecorder()

			api.HandleSlackStatus(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rr.Code)
			}
			var body map[string]string
			if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
				t.Fatalf("invalid json: %v", err)
			}
			if body["status"] != tt.want {
				t.Errorf("expected status=%q, got %q", tt.want, body["status"])
			}
		})
	}
}

func TestHandleGetReleaseNotes_Validation(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  int
	}{
		{"invalid type rejected", "?type=hack&lang=en", http.StatusBadRequest},
		{"non-alpha lang rejected", "?type=user&lang=e1", http.StatusBadRequest},
		{"oversize lang rejected", "?type=user&lang=koor", http.StatusBadRequest},
	}
	api := &API{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/release-notes"+tt.query, nil)
			rr := httptest.NewRecorder()

			api.HandleGetReleaseNotes(rr, req)

			if rr.Code != tt.want {
				t.Errorf("expected %d, got %d (body=%s)", tt.want, rr.Code, rr.Body.String())
			}
		})
	}
}
