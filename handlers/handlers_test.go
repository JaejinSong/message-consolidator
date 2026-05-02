package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

func TestHandleHealth(t *testing.T) {
	api := &API{}
	req := httptest.NewRequest("GET", "/api/health", nil)
	rr := httptest.NewRecorder()

	api.HandleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body["status"] != "OK" {
		t.Errorf("expected status=OK, got %q", body["status"])
	}
}

func TestNewAPI_WiresFields(t *testing.T) {
	t.Parallel()
	scan := func(string, string) {}
	full := func() {}
	api := NewAPI(nil, scan, full, nil, nil, nil, nil)
	if api == nil {
		t.Fatal("NewAPI returned nil")
	}
	if api.ScanFunc == nil || api.FullScanFunc == nil {
		t.Error("NewAPI did not wire scan funcs")
	}
}

func TestBatchIDsRequest_GetIDs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		req  BatchIDsRequest
		want []store.MessageID
	}{
		{"empty falls through", BatchIDsRequest{}, nil},
		{"slice form preserved", BatchIDsRequest{IDs: []store.MessageID{1, 2, 3}}, []store.MessageID{1, 2, 3}},
		{"single id promoted to slice", BatchIDsRequest{ID: 42}, []store.MessageID{42}},
		{"slice wins over single id", BatchIDsRequest{IDs: []store.MessageID{1}, ID: 99}, []store.MessageID{1}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.req.GetIDs()
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for i, id := range tt.want {
				if got[i] != id {
					t.Errorf("[%d] = %d, want %d", i, got[i], id)
				}
			}
		})
	}
}

func TestBatchIDsRequest_Validate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		req     BatchIDsRequest
		wantErr bool
	}{
		{"empty rejected", BatchIDsRequest{}, true},
		{"single positive accepted", BatchIDsRequest{ID: 1}, false},
		{"slice positive accepted", BatchIDsRequest{IDs: []store.MessageID{1, 2}}, false},
		{"zero rejected", BatchIDsRequest{IDs: []store.MessageID{0}}, true},
		{"negative rejected", BatchIDsRequest{IDs: []store.MessageID{-3}}, true},
		{"any non-positive in slice rejected", BatchIDsRequest{IDs: []store.MessageID{1, -1, 2}}, true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.req.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestParsePathID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		vars    map[string]string
		key     string
		wantID  int64
		wantErr bool
	}{
		{"missing returns error", map[string]string{}, "id", 0, true},
		{"non-numeric returns error", map[string]string{"id": "abc"}, "id", 0, true},
		{"numeric parsed", map[string]string{"id": "123"}, "id", 123, false},
		{"negative parsed", map[string]string{"id": "-5"}, "id", -5, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := mux.SetURLVars(httptest.NewRequest("GET", "/x", nil), tt.vars)
			got, err := parsePathID(req, tt.key)
			if tt.wantErr && err == nil {
				t.Error("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if got != tt.wantID {
				t.Errorf("id = %d, want %d", got, tt.wantID)
			}
		})
	}
}

func TestParseBatchIDs(t *testing.T) {
	t.Parallel()
	t.Run("invalid json -> 400 false", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest("POST", "/x", strings.NewReader("{bad"))
		rr := httptest.NewRecorder()
		ids, ok := parseBatchIDs(rr, req)
		if ok || ids != nil {
			t.Errorf("got ok=%v ids=%v, want false/nil", ok, ids)
		}
		if rr.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rr.Code)
		}
	})
	t.Run("empty ids -> 400 false", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest("POST", "/x", strings.NewReader(`{}`))
		rr := httptest.NewRecorder()
		_, ok := parseBatchIDs(rr, req)
		if ok {
			t.Error("expected false for empty ids")
		}
		if rr.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rr.Code)
		}
	})
	t.Run("valid passes", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest("POST", "/x", strings.NewReader(`{"ids":[1,2]}`))
		rr := httptest.NewRecorder()
		ids, ok := parseBatchIDs(rr, req)
		if !ok || len(ids) != 2 {
			t.Errorf("got ok=%v ids=%v, want true/[1 2]", ok, ids)
		}
	})
}

func TestBindJSON(t *testing.T) {
	t.Parallel()
	t.Run("valid body decoded", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest("POST", "/x", strings.NewReader(`{"name":"alice"}`))
		rr := httptest.NewRecorder()
		var dst struct {
			Name string `json:"name"`
		}
		if !bindJSON(rr, req, &dst) {
			t.Fatal("expected true on valid JSON")
		}
		if dst.Name != "alice" {
			t.Errorf("name = %q, want alice", dst.Name)
		}
	})
	t.Run("invalid body writes 400", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest("POST", "/x", strings.NewReader("not json"))
		rr := httptest.NewRecorder()
		var dst map[string]string
		if bindJSON(rr, req, &dst) {
			t.Fatal("expected false on invalid JSON")
		}
		if rr.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rr.Code)
		}
	})
}

func TestRespondJSON_AndError(t *testing.T) {
	t.Parallel()
	t.Run("respondJSON sets content-type and status", func(t *testing.T) {
		t.Parallel()
		rr := httptest.NewRecorder()
		respondJSON(rr, http.StatusCreated, map[string]int{"n": 1})
		if rr.Code != http.StatusCreated {
			t.Errorf("status = %d, want 201", rr.Code)
		}
		if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q, want application/json", ct)
		}
		var body map[string]int
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil || body["n"] != 1 {
			t.Errorf("body = %s err=%v", rr.Body.String(), err)
		}
	})
	t.Run("respondError emits error envelope", func(t *testing.T) {
		t.Parallel()
		rr := httptest.NewRecorder()
		respondError(rr, http.StatusUnauthorized, "nope")
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rr.Code)
		}
		var body map[string]string
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		if body["error"] != "nope" {
			t.Errorf("error body = %v", body)
		}
	})
	t.Run("unmarshalable payload returns 500", func(t *testing.T) {
		t.Parallel()
		// Why: channels do not survive json.Marshal — exercise the error branch.
		rr := httptest.NewRecorder()
		respondJSON(rr, http.StatusOK, make(chan int))
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rr.Code)
		}
	})
}

func TestHandleAPIError(t *testing.T) {
	t.Parallel()
	t.Run("context.Canceled returns 499", func(t *testing.T) {
		t.Parallel()
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/x", nil)
		handleAPIError(rr, req, context.Canceled, "[X]", "msg")
		if rr.Code != 499 {
			t.Errorf("status = %d, want 499", rr.Code)
		}
	})
	t.Run("generic error returns 500 with message", func(t *testing.T) {
		t.Parallel()
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/x", nil)
		handleAPIError(rr, req, errors.New("boom"), "[X]", "outer")
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "outer") {
			t.Errorf("body = %s, expected 'outer'", rr.Body.String())
		}
	})
}

func TestDecodeJSON_ClosesBody(t *testing.T) {
	t.Parallel()
	body := &countingCloser{Reader: strings.NewReader(`{"k":1}`)}
	req, _ := http.NewRequest("POST", "/x", body)
	var dst map[string]int
	if err := decodeJSON(req, &dst); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if !body.closed {
		t.Error("decodeJSON did not close request body")
	}
}

type countingCloser struct {
	io.Reader
	closed bool
}

func (c *countingCloser) Close() error { c.closed = true; return nil }

func TestHTTPError(t *testing.T) {
	t.Parallel()
	err := httpError("bad input")
	if err == nil || err.Error() != "bad input" {
		t.Errorf("httpError = %v, want 'bad input'", err)
	}
}
