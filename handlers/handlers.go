package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"message-consolidator/config"
	"message-consolidator/logger"
	"message-consolidator/services"
	"net/http"
)

// API holds all dependencies for API handlers, promoting testability and avoiding global state.
type API struct {
	Config       *config.Config
	ScanFunc     func(email string, lang string)
	FullScanFunc func()
	Reports      *services.ReportsService
	Tasks        *services.TasksService
	//Why: Allows for future expansion of shared dependencies like database stores or logger instances.
}

// NewAPI is a constructor for the API struct, making dependency injection explicit.
func NewAPI(cfg *config.Config, scanFunc func(string, string), fullScanFunc func(), reports *services.ReportsService, tasks *services.TasksService) *API {
	return &API{
		Config:       cfg,
		ScanFunc:     scanFunc,
		FullScanFunc: fullScanFunc,
		Reports:      reports,
		Tasks:        tasks,
	}
}

// HandleHealth provides a simple health check endpoint.
// It's now a method on the API struct, allowing access to dependencies if needed.
func (a *API) HandleHealth(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{"status": "OK"})
}

// respondError is a helper function for sending consistent JSON error responses.
func respondError(w http.ResponseWriter, code int, message string) {
	//Why: Provides a centralized location for future error logging and monitoring integration.
	// logger.Errorf("API Error: %s", message)
	respondJSON(w, code, map[string]string{"error": message})
}

// handleAPIError handles common API errors including context cancellation.
// Why: [DRY] Gracefully handles Turso DB's 'context canceled' to prevent 500 error logs and returns 499 for client cancellations.
func handleAPIError(w http.ResponseWriter, r *http.Request, err error, logPrefix, errMsg string) {
	if errors.Is(err, context.Canceled) {
		// HTTP 499 Client Closed Request
		w.WriteHeader(499)
		return
	}
	logger.Errorf("%s: %v", logPrefix, err)
	respondError(w, http.StatusInternalServerError, errMsg)
}

// respondJSON is a helper function to marshal data and send a JSON response.
func respondJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, err := json.Marshal(payload)
	if err != nil {
		logger.Errorf("Internal Server Error: Failed to marshal JSON response: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}

// BatchIDsRequest is a common DTO for operations targeting multiple message IDs.
// Why: [DRY] Consolidates request parsing and validation for deletion, restoration, and batch updates.
type BatchIDsRequest struct {
	IDs []int `json:"ids"`
	ID  int   `json:"id"` // Fallback for single ID operations
}

// GetIDs normalizes single and multiple ID inputs into a uniform slice.
func (r *BatchIDsRequest) GetIDs() []int {
	if len(r.IDs) == 0 && r.ID != 0 {
		return []int{r.ID}
	}
	return r.IDs
}

// Validate ensures all provided IDs are strictly positive integers.
// Why: [Explicit Integer Conversion] and [Guard Clauses] prevent malformed or invalid database queries.
func (r *BatchIDsRequest) Validate() error {
	ids := r.GetIDs()
	if len(ids) == 0 {
		return errors.New("no valid IDs provided")
	}
	for _, id := range ids {
		if id <= 0 {
			return fmt.Errorf("invalid ID: %d (must be > 0)", id)
		}
	}
	return nil
}

// decodeJSON is a common helper that parses JSON from an HTTP request and safely closes the Body to prevent memory leaks.
// Why: [DRY] Centralizes JSON decoding logic used across multiple handler files.
func decodeJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
