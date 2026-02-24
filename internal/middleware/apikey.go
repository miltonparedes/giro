// Package middleware provides HTTP middleware for the giro proxy.
package middleware

import (
	"encoding/json"
	"net/http"
)

// openAIAuthError is the JSON error shape returned by the OpenAI-compatible
// auth middleware on 401 responses.
type openAIAuthError struct {
	Error openAIAuthErrorBody `json:"error"`
}

// openAIAuthErrorBody holds the inner fields of an OpenAI auth error.
type openAIAuthErrorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    int    `json:"code"`
}

// anthropicAuthError is the JSON error shape returned by the Anthropic-compatible
// auth middleware on 401 responses.
type anthropicAuthError struct {
	Type  string                 `json:"type"`
	Error anthropicAuthErrorBody `json:"error"`
}

// anthropicAuthErrorBody holds the inner fields of an Anthropic auth error.
type anthropicAuthErrorBody struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// OpenAIAuth returns a middleware that validates requests against the OpenAI
// Bearer token convention. When proxyAPIKey is empty authentication is
// disabled and all requests are allowed through.
func OpenAIAuth(proxyAPIKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if proxyAPIKey == "" {
				next.ServeHTTP(w, r)
				return
			}

			auth := r.Header.Get("Authorization")
			if auth == "Bearer "+proxyAPIKey {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(openAIAuthError{
				Error: openAIAuthErrorBody{
					Message: "Invalid or missing API Key",
					Type:    "authentication_error",
					Code:    http.StatusUnauthorized,
				},
			})
		})
	}
}

// AnthropicAuth returns a middleware that validates requests against the
// Anthropic API key conventions. It accepts the key via the x-api-key header
// or as a Bearer token in the Authorization header. When proxyAPIKey is empty
// authentication is disabled and all requests are allowed through.
func AnthropicAuth(proxyAPIKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if proxyAPIKey == "" {
				next.ServeHTTP(w, r)
				return
			}

			if r.Header.Get("x-api-key") == proxyAPIKey {
				next.ServeHTTP(w, r)
				return
			}

			if r.Header.Get("Authorization") == "Bearer "+proxyAPIKey {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(anthropicAuthError{
				Type: "error",
				Error: anthropicAuthErrorBody{
					Type:    "authentication_error",
					Message: "Invalid or missing API key. Use x-api-key header or Authorization: Bearer.",
				},
			})
		})
	}
}
