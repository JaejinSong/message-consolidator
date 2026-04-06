package handlers

import (
	"context"
	"message-consolidator/auth"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
