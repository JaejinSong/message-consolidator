package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// NewChatCompletionServer starts an OpenAI-compatible stub and returns its base URL.
// Why: the app's only LLM transport speaks chat completions, so tests fake the provider at the
// HTTP edge; reply returns the assistant text, and an error becomes an HTTP 500.
func NewChatCompletionServer(t testing.TB, reply func(r *http.Request) (string, error)) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		text, err := reply(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]string{"role": "assistant", "content": text},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1},
		})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}
