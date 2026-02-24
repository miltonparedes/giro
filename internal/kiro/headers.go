// Package kiro provides Kiro API client utilities: headers, HTTP client, and stream parsing.
package kiro

import (
	"net/http"

	"github.com/google/uuid"
)

// GetKiroHeaders returns the standard headers for Kiro API requests.
func GetKiroHeaders(fingerprint, token string) http.Header {
	h := make(http.Header)
	h.Set("Authorization", "Bearer "+token)
	h.Set("Content-Type", "application/json")
	h.Set("User-Agent", "aws-sdk-js/1.0.27 ua/2.1 os/win32#10.0.19044 lang/js md/nodejs#22.21.1 api/codewhispererstreaming#1.0.27 m/E KiroIDE-0.7.45-"+fingerprint)
	h.Set("X-Amz-User-Agent", "aws-sdk-js/1.0.27 KiroIDE-0.7.45-"+fingerprint)
	h.Set("X-Amzn-Codewhisperer-Optout", "true")
	h.Set("X-Amzn-Kiro-Agent-Mode", "vibe")
	h.Set("Amz-Sdk-Invocation-Id", uuid.NewString())
	h.Set("Amz-Sdk-Request", "attempt=1; max=3")
	return h
}
