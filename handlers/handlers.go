package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"message-consolidator/ai"
	"message-consolidator/config"
	"message-consolidator/logger"
	"message-consolidator/services"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

// API holds all dependencies for API handlers, promoting testability and avoiding global state.
type API struct {
	Config           *config.Config
	ScanFunc         func(email string, lang string)
	FullScanFunc     func()
	Reports          *services.ReportsService
	Tasks            *services.TasksService
	IdentityResolver *ai.IdentityResolver
}

// NewAPI is a constructor for the API struct, making dependency injection explicit.
func NewAPI(cfg *config.Config, scanFunc func(string, string), fullScanFunc func(), reports *services.ReportsService, tasks *services.TasksService, identityResolver *ai.IdentityResolver) *API {
	return &API{
		Config:           cfg,
		ScanFunc:         scanFunc,
		FullScanFunc:     fullScanFunc,
		Reports:          reports,
		Tasks:            tasks,
		IdentityResolver: identityResolver,
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
	_, _ = w.Write(response)
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

// parsePathID extracts and parses an integer path variable by key.
func parsePathID(r *http.Request, key string) (int, error) {
	idStr := mux.Vars(r)[key]
	if idStr == "" {
		return 0, httpError("Missing " + key)
	}
	id64, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0, err
	}
	return int(id64), nil
}

// parseBatchIDs decodes and validates a BatchIDsRequest, writing error responses on failure.
func parseBatchIDs(w http.ResponseWriter, r *http.Request) ([]int, bool) {
	var req BatchIDsRequest
	if !bindJSON(w, r, &req) {
		return nil, false
	}
	if err := req.Validate(); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	return req.GetIDs(), true
}

// decodeJSON is a common helper that parses JSON from an HTTP request and safely closes the Body to prevent memory leaks.
// Why: [DRY] Centralizes JSON decoding logic used across multiple handler files.
func decodeJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// bindJSON decodes the request body and writes a 400 response on failure. Returns false if decoding failed.
func bindJSON(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if err := decodeJSON(r, v); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}
