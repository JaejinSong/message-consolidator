package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleTelegramSetCredentials_Validation(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{"invalid json -> 400", `{not-json`, http.StatusBadRequest},
		{"missing app_id -> 400", `{"app_hash":"abc"}`, http.StatusBadRequest},
		{"missing app_hash -> 400", `{"app_id":12345}`, http.StatusBadRequest},
		{"negative app_id -> 400", `{"app_id":-1,"app_hash":"abc"}`, http.StatusBadRequest},
		{"whitespace app_hash -> 400", `{"app_id":12345,"app_hash":"   "}`, http.StatusBadRequest},
	}
	api := &API{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("POST", "/api/channels/telegram/credentials", strings.NewReader(tt.body))
			req = req.WithContext(WithMockUser(req.Context(), "u@example.com"))
			rr := httptest.NewRecorder()

			api.HandleTelegramSetCredentials(rr, req)

			if rr.Code != tt.want {
				t.Errorf("got %d want %d (body=%s)", rr.Code, tt.want, rr.Body.String())
			}
		})
	}
}

func TestMaskTelegramPhone(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty -> empty", "", ""},
		{"too short -> empty", "+82", ""},
		{"long phone -> prefix + mask + last4", "+821012345678", "+82****5678"},
		{"short with last4", "12345", "****2345"},
		{"trims whitespace", "  +821055556789  ", "+82****6789"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maskTelegramPhone(tt.in); got != tt.want {
				t.Errorf("maskTelegramPhone(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMaskTelegramAppID(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want string
	}{
		{"zero -> empty", 0, ""},
		{"negative -> empty", -5, ""},
		{"short id padded", 42, "42***"},
		{"long id truncated", 1234567, "123***"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maskTelegramAppID(tt.in); got != tt.want {
				t.Errorf("maskTelegramAppID(%d) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestHandleTelegramAuthStart_Validation(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{"invalid json -> 400", `{`, http.StatusBadRequest},
		{"empty phone -> 400", `{"phone":""}`, http.StatusBadRequest},
		{"whitespace phone -> 400", `{"phone":"   "}`, http.StatusBadRequest},
	}
	api := &API{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("POST", "/api/channels/telegram/auth/start", bytes.NewBufferString(tt.body))
			req = req.WithContext(WithMockUser(req.Context(), "u@example.com"))
			rr := httptest.NewRecorder()

			api.HandleTelegramAuthStart(rr, req)

			if rr.Code != tt.want {
				t.Errorf("got %d want %d (body=%s)", rr.Code, tt.want, rr.Body.String())
			}
		})
	}
}
