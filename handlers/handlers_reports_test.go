package handlers

import (
	"context"
	"message-consolidator/auth"
	"message-consolidator/config"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

// withReportID injects mux path vars + auth user into a request.
func withReportID(req *http.Request, id, email string) *http.Request {
	req = mux.SetURLVars(req, map[string]string{"id": id})
	return req.WithContext(context.WithValue(req.Context(), auth.UserEmailKey, email))
}

func TestHandleGenerateReport_LangValidation(t *testing.T) {
	// Why: Mock API instance with nil services as we are testing the handler's validation logic
	api := &API{
		Reports: nil, // Should trigger 503 if validation passes
	}

	tests := []struct {
		name           string
		query          string
		expectedStatus int
	}{
		{
			name:           "Missing lang parameter -> 400 Bad Request",
			query:          "?start=2024-01-01&end=2024-01-07",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Empty lang parameter -> 400 Bad Request",
			query:          "?lang=&start=2024-01-01&end=2024-01-07",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Invalid start date -> 400 Bad Request",
			query:          "?lang=ko&start=invalid&end=2024-01-07",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Valid parameters but missing Reports service -> 503 Service Unavailable",
			query:          "?lang=en&start=2024-01-01&end=2024-01-07",
			expectedStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/reports"+tt.query, nil)
			
			// Why: Inject mock user email into context to bypass auth check in handler
			ctx := context.WithValue(req.Context(), auth.UserEmailKey, "test@example.com")
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()

			api.HandleGenerateReport(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("%s: expected status code %d, got %d", tt.name, tt.expectedStatus, rr.Code)
			}
		})
	}
}

func TestHandleGetReportByID_Validation(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	api := &API{}
	tests := []struct {
		name string
		id   string
		want int
	}{
		{"missing id -> 400", "", http.StatusBadRequest},
		{"non-numeric id -> 400", "abc", http.StatusBadRequest},
		{"unknown id -> 404", "999999", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := withReportID(httptest.NewRequest("GET", "/api/reports/"+tt.id, nil), tt.id, "u@example.com")
			rr := httptest.NewRecorder()
			api.HandleGetReportByID(rr, req)
			if rr.Code != tt.want {
				t.Errorf("got %d want %d (body=%s)", rr.Code, tt.want, rr.Body.String())
			}
		})
	}
}

func TestHandleDeleteReport_Validation(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	api := &API{}
	tests := []struct {
		name string
		id   string
		want int
	}{
		{"non-numeric id -> 400", "abc", http.StatusBadRequest},
		{"unknown id -> 404", "777777", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := withReportID(httptest.NewRequest("DELETE", "/api/reports/"+tt.id, nil), tt.id, "u@example.com")
			rr := httptest.NewRecorder()
			api.HandleDeleteReport(rr, req)
			if rr.Code != tt.want {
				t.Errorf("got %d want %d (body=%s)", rr.Code, tt.want, rr.Body.String())
			}
		})
	}
}

func TestHandleTranslateReport_Validation(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		query   string
		reports bool // whether to inject Reports service
		want    int
	}{
		{"non-numeric id -> 400", "abc", "?lang=en", false, http.StatusBadRequest},
		{"missing lang -> 400", "1", "", false, http.StatusBadRequest},
		{"empty lang -> 400", "1", "?lang=", false, http.StatusBadRequest},
		{"valid params but no Reports service -> 503", "1", "?lang=en", false, http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &API{}
			req := withReportID(httptest.NewRequest("POST", "/api/reports/"+tt.id+"/translate"+tt.query, nil), tt.id, "u@example.com")
			rr := httptest.NewRecorder()
			api.HandleTranslateReport(rr, req)
			if rr.Code != tt.want {
				t.Errorf("got %d want %d (body=%s)", rr.Code, tt.want, rr.Body.String())
			}
		})
	}
}

func TestHandleExportReportToNotion_Validation(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	api := &API{Config: &config.Config{}} // empty NotionToken/PageID
	tests := []struct {
		name string
		id   string
		want int
	}{
		{"non-numeric id -> 400", "abc", http.StatusBadRequest},
		{"unknown report id -> 404", "999999", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := withReportID(httptest.NewRequest("POST", "/api/reports/"+tt.id+"/notion", nil), tt.id, "u@example.com")
			rr := httptest.NewRecorder()
			api.HandleExportReportToNotion(rr, req)
			if rr.Code != tt.want {
				t.Errorf("got %d want %d (body=%s)", rr.Code, tt.want, rr.Body.String())
			}
		})
	}
}
