package handlers

import (
	"encoding/json"
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
	//Why: Allows for future expansion of shared dependencies like database stores or logger instances.
	// Store        store.Store
}

// NewAPI is a constructor for the API struct, making dependency injection explicit.
func NewAPI(cfg *config.Config, scanFunc func(string, string), fullScanFunc func(), reports *services.ReportsService) *API {
	return &API{
		Config:       cfg,
		ScanFunc:     scanFunc,
		FullScanFunc: fullScanFunc,
		Reports:      reports,
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
