package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/miltonparedes/giro/internal/middleware"
)

// dummyHandler is a simple 200-OK handler used as the "next" handler in tests.
func dummyHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// --- OpenAIAuth --------------------------------------------------------

func TestOpenAIAuth_ValidBearer(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.OpenAIAuth("test-key"))
	r.Get("/", dummyHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestOpenAIAuth_MissingHeader(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.OpenAIAuth("test-key"))
	r.Get("/", dummyHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}

	var body map[string]map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	errObj, ok := body["error"]
	if !ok {
		t.Fatal("response missing 'error' key")
	}
	if errObj["message"] != "Invalid or missing API Key" {
		t.Fatalf("unexpected message: %v", errObj["message"])
	}
	if errObj["type"] != "authentication_error" {
		t.Fatalf("unexpected type: %v", errObj["type"])
	}
	if int(errObj["code"].(float64)) != 401 {
		t.Fatalf("unexpected code: %v", errObj["code"])
	}
}

func TestOpenAIAuth_WrongKey(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.OpenAIAuth("test-key"))
	r.Get("/", dummyHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestOpenAIAuth_EmptyKeyAllowsAll(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.OpenAIAuth(""))
	r.Get("/", dummyHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 when key is empty, got %d", rr.Code)
	}
}

// --- AnthropicAuth -----------------------------------------------------

func TestAnthropicAuth_ValidXAPIKey(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.AnthropicAuth("test-key"))
	r.Get("/", dummyHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("x-api-key", "test-key")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestAnthropicAuth_ValidBearer(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.AnthropicAuth("test-key"))
	r.Get("/", dummyHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestAnthropicAuth_MissingBothHeaders(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.AnthropicAuth("test-key"))
	r.Get("/", dummyHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["type"] != "error" {
		t.Fatalf("expected top-level type 'error', got %v", body["type"])
	}

	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("response missing 'error' object")
	}
	if errObj["type"] != "authentication_error" {
		t.Fatalf("unexpected error type: %v", errObj["type"])
	}
	if errObj["message"] != "Invalid or missing API key. Use x-api-key header or Authorization: Bearer." {
		t.Fatalf("unexpected message: %v", errObj["message"])
	}
}

func TestAnthropicAuth_WrongKey(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.AnthropicAuth("test-key"))
	r.Get("/", dummyHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("x-api-key", "wrong-key")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAnthropicAuth_EmptyKeyAllowsAll(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.AnthropicAuth(""))
	r.Get("/", dummyHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 when key is empty, got %d", rr.Code)
	}
}
