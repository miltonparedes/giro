package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/miltonparedes/giro/internal/auth"
	"github.com/miltonparedes/giro/internal/config"
	"github.com/miltonparedes/giro/internal/model"
)

func newAuthManagerWithOverrides(t *testing.T, apiHost, qHost string) *auth.KiroAuthManager {
	t.Helper()

	credsPath := filepath.Join(t.TempDir(), "credentials.json")
	expiresAt := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)
	creds := fmt.Sprintf(
		`{"accessToken":"test-access-token","refreshToken":"test-refresh-token","expiresAt":%q}`,
		expiresAt,
	)
	if err := os.WriteFile(credsPath, []byte(creds), 0o600); err != nil {
		t.Fatalf("write creds file: %v", err)
	}

	m, err := auth.NewKiroAuthManager(auth.Options{
		Region:          "us-east-1",
		CredsFile:       credsPath,
		APIHostOverride: apiHost,
		QHostOverride:   qHost,
	})
	if err != nil {
		t.Fatalf("NewKiroAuthManager: %v", err)
	}

	return m
}

func assertFallbackModelsLoaded(t *testing.T, cache *model.InfoCache) {
	t.Helper()

	for _, id := range config.FallbackModels {
		info, ok := cache.Get(id)
		if !ok {
			t.Fatalf("fallback model %q not loaded", id)
		}
		if info.MaxInputTokens != config.DefaultMaxInputTokens {
			t.Fatalf("fallback model %q max tokens = %d, want %d", id, info.MaxInputTokens, config.DefaultMaxInputTokens)
		}
	}
}

func TestFetchModels_Success(t *testing.T) {
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ListAvailableModels" {
			t.Fatalf("path = %q, want /ListAvailableModels", r.URL.Path)
		}
		if got := r.URL.Query().Get("origin"); got != "AI_EDITOR" {
			t.Fatalf("origin query = %q, want AI_EDITOR", got)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"models":[{"modelId":"claude-sonnet-4","tokenLimits":{"maxInputTokens":1234}},{"modelId":"claude-haiku-4.5","tokenLimits":{"maxInputTokens":4321}}]}`)
	}))
	defer q.Close()

	authManager := newAuthManagerWithOverrides(t, q.URL, q.URL)
	cache := model.NewInfoCache(time.Hour)

	fetchModels(context.Background(), authManager, cache, &http.Client{Timeout: 2 * time.Second})

	info, ok := cache.Get("claude-sonnet-4")
	if !ok {
		t.Fatal("expected claude-sonnet-4 in cache")
	}
	if info.MaxInputTokens != 1234 {
		t.Fatalf("maxInputTokens = %d, want 1234", info.MaxInputTokens)
	}

	info, ok = cache.Get("claude-haiku-4.5")
	if !ok {
		t.Fatal("expected claude-haiku-4.5 in cache")
	}
	if info.MaxInputTokens != 4321 {
		t.Fatalf("maxInputTokens = %d, want 4321", info.MaxInputTokens)
	}
}

func TestFetchModels_TokenError_UsesFallback(t *testing.T) {
	authManager, err := auth.NewKiroAuthManager(auth.Options{})
	if err != nil {
		t.Fatalf("NewKiroAuthManager: %v", err)
	}

	cache := model.NewInfoCache(time.Hour)
	fetchModels(context.Background(), authManager, cache, &http.Client{Timeout: 2 * time.Second})

	assertFallbackModelsLoaded(t, cache)
}

func TestFetchModels_Non200_UsesFallback(t *testing.T) {
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "upstream error")
	}))
	defer q.Close()

	authManager := newAuthManagerWithOverrides(t, q.URL, q.URL)
	cache := model.NewInfoCache(time.Hour)

	fetchModels(context.Background(), authManager, cache, &http.Client{Timeout: 2 * time.Second})

	assertFallbackModelsLoaded(t, cache)
}

func TestFetchModels_InvalidJSON_UsesFallback(t *testing.T) {
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"models":[`) // intentionally malformed
	}))
	defer q.Close()

	authManager := newAuthManagerWithOverrides(t, q.URL, q.URL)
	cache := model.NewInfoCache(time.Hour)

	fetchModels(context.Background(), authManager, cache, &http.Client{Timeout: 2 * time.Second})

	assertFallbackModelsLoaded(t, cache)
}

func TestPopulateFallbackModels(t *testing.T) {
	cache := model.NewInfoCache(time.Hour)
	populateFallbackModels(cache)
	assertFallbackModelsLoaded(t, cache)
}
