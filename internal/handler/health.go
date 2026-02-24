// Package handler contains HTTP handlers.
package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/miltonparedes/giro/internal/config"
)

// Health responds with a JSON status indicating the server is running.
func Health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"version": config.AppVersion,
	})
}

// DetailedHealth responds with extended health information including a timestamp.
func DetailedHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"version":   config.AppVersion,
	})
}

// writeJSONError writes a JSON error response with the given status code and body.
func writeJSONError(w http.ResponseWriter, statusCode int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(body)
}
