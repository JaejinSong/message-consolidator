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
