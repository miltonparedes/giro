package handler

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/miltonparedes/giro/internal/kiro"
)

func TestKiroErrorStatus_UsesUpstreamStatus(t *testing.T) {
	err := &kiro.KiroHTTPError{StatusCode: http.StatusUnauthorized, Message: "bad token"}

	got := kiroErrorStatus(err)

	if got != http.StatusUnauthorized {
		t.Fatalf("kiroErrorStatus = %d, want %d", got, http.StatusUnauthorized)
	}
}

func TestKiroErrorStatus_UsesWrappedUpstreamStatus(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &kiro.KiroHTTPError{
		StatusCode: http.StatusBadRequest,
		Message:    "bad payload",
	})

	got := kiroErrorStatus(err)

	if got != http.StatusBadRequest {
		t.Fatalf("kiroErrorStatus = %d, want %d", got, http.StatusBadRequest)
	}
}

func TestKiroErrorStatus_FallbacksToBadGateway(t *testing.T) {
	got := kiroErrorStatus(errors.New("plain network error"))

	if got != http.StatusBadGateway {
		t.Fatalf("kiroErrorStatus = %d, want %d", got, http.StatusBadGateway)
	}
}

func TestKiroErrorStatus_InvalidStatusFallback(t *testing.T) {
	got := kiroErrorStatus(&kiro.KiroHTTPError{StatusCode: 0, Message: "invalid"})

	if got != http.StatusBadGateway {
		t.Fatalf("kiroErrorStatus = %d, want %d", got, http.StatusBadGateway)
	}
}
