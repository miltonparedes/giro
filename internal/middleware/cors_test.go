package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/miltonparedes/giro/internal/middleware"
)

func TestCORS_OptionsRequest(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.CORS())
	r.Get("/", dummyHandler)

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}

	expectedHeaders := map[string]string{
		"Access-Control-Allow-Origin":      "*",
		"Access-Control-Allow-Methods":     "*",
		"Access-Control-Allow-Headers":     "*",
		"Access-Control-Allow-Credentials": "true",
		"Access-Control-Max-Age":           "86400",
	}

	for header, want := range expectedHeaders {
		got := rr.Header().Get(header)
		if got != want {
			t.Errorf("header %s: expected %q, got %q", header, want, got)
		}
	}
}

func TestCORS_GetRequest(t *testing.T) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	r := chi.NewRouter()
	r.Use(middleware.CORS())
	r.Get("/", handler.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if !called {
		t.Fatal("expected handler to be called")
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("expected Access-Control-Allow-Origin *, got %q", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("expected Access-Control-Allow-Credentials true, got %q", got)
	}
}

func TestCORS_PostRequest(t *testing.T) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	})

	r := chi.NewRouter()
	r.Use(middleware.CORS())
	r.Post("/", handler.ServeHTTP)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if !called {
		t.Fatal("expected handler to be called")
	}
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("expected Access-Control-Allow-Origin *, got %q", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("expected Access-Control-Allow-Credentials true, got %q", got)
	}
}
