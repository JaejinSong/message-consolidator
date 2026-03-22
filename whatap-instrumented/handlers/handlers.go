package handlers

import (
	"message-consolidator/config"
	"net/http"
)

var cfg *config.Config

func Init(c *config.Config) {
	cfg = c
}

// HandleHealth provides a simple health check endpoint.
func HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
