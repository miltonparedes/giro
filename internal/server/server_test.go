package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miltonparedes/giro/internal/auth"
	"github.com/miltonparedes/giro/internal/config"
	"github.com/miltonparedes/giro/internal/model"
)

func newTestAuthManager(t *testing.T) *auth.KiroAuthManager {
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
		APIHostOverride: "http://127.0.0.1:1",
		QHostOverride:   "http://127.0.0.1:1",
	})
	if err != nil {
		t.Fatalf("NewKiroAuthManager: %v", err)
	}

	return m
}

func newTestResolver() *model.Resolver {
	cache := model.NewInfoCache(time.Hour)
	cache.Update([]model.Info{{ModelID: "claude-sonnet-4", MaxInputTokens: config.DefaultMaxInputTokens}})
	return model.NewResolver(cache, map[string]string{}, map[string]string{}, nil)
}

func newRouter(t *testing.T) http.Handler {
	t.Helper()

	cfg := config.Config{ProxyAPIKey: "test-key"}
	return New(cfg, newTestAuthManager(t), newTestResolver(), &http.Client{Timeout: 2 * time.Second})
}

func TestNew_HealthEndpoints_NoAuth(t *testing.T) {
	r := newRouter(t)

	for _, path := range []string{"/", "/health"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("%s status = %d, want %d", path, rr.Code, http.StatusOK)
			}
		})
	}
}

func TestNew_Models_RequiresAuth(t *testing.T) {
	r := newRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestNew_Models_BearerAuth(t *testing.T) {
	r := newRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !strings.Contains(rr.Body.String(), "claude-sonnet-4") {
		t.Fatalf("expected model list in response body, got %q", rr.Body.String())
	}
}

func TestNew_AnthropicAuth_XAPIKeyAndBearer(t *testing.T) {
	r := newRouter(t)
	body := "{"

	t.Run("x-api-key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
		req.Header.Set("x-api-key", "test-key")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code == http.StatusUnauthorized {
			t.Fatalf("status = %d, expected non-401 with valid x-api-key", rr.Code)
		}
	})

	t.Run("bearer", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-key")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code == http.StatusUnauthorized {
			t.Fatalf("status = %d, expected non-401 with valid bearer key", rr.Code)
		}
	})
}

func TestNew_CORSPreflight(t *testing.T) {
	r := newRouter(t)

	req := httptest.NewRequest(http.MethodOptions, "/v1/models", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNoContent)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "https://example.com")
	}
	if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("Access-Control-Allow-Credentials = %q, want %q", got, "true")
	}
}
