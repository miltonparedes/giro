package server

import (
	"encoding/json"
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

func newTestAuthManagerWithUpstream(t *testing.T, upstream string) *auth.KiroAuthManager {
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
		APIHostOverride: upstream,
		QHostOverride:   upstream,
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

// Bearer client auth rejection returns OpenAI error envelope.
func TestNew_OpenAI_AuthRejection_ErrorEnvelope(t *testing.T) {
	r := newRouter(t)

	for _, tc := range []struct {
		name   string
		method string
		path   string
		auth   string
	}{
		{"models_no_auth", http.MethodGet, "/v1/models", ""},
		{"models_wrong_key", http.MethodGet, "/v1/models", "Bearer wrong-key"},
		{"completions_no_auth", http.MethodPost, "/v1/chat/completions", ""},
		{"completions_wrong_key", http.MethodPost, "/v1/chat/completions", "Bearer wrong-key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
			}

			var body map[string]any
			if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}

			errObj, ok := body["error"].(map[string]any)
			if !ok {
				t.Fatal("response missing 'error' object")
			}
			if errObj["message"] == nil || errObj["message"] == "" {
				t.Fatal("error.message is empty or missing")
			}
			if errObj["type"] != "authentication_error" {
				t.Fatalf("error.type = %v, want authentication_error", errObj["type"])
			}
			if code, _ := errObj["code"].(float64); int(code) != http.StatusUnauthorized {
				t.Fatalf("error.code = %v, want %d", errObj["code"], http.StatusUnauthorized)
			}
		})
	}
}

// /v1/models returns a valid OpenAI model-list envelope.
func TestNew_Models_Envelope(t *testing.T) {
	r := newRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body["object"] != "list" {
		t.Fatalf("object = %v, want list", body["object"])
	}

	data, ok := body["data"].([]any)
	if !ok || len(data) == 0 {
		t.Fatal("data must be a non-empty array")
	}

	for i, item := range data {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("data[%d] is not an object", i)
		}
		if m["id"] == nil || m["id"] == "" {
			t.Fatalf("data[%d].id is empty", i)
		}
		if m["object"] != "model" {
			t.Fatalf("data[%d].object = %v, want model", i, m["object"])
		}
		if m["created"] == nil {
			t.Fatalf("data[%d].created is missing", i)
		}
		if m["owned_by"] == nil || m["owned_by"] == "" {
			t.Fatalf("data[%d].owned_by is empty", i)
		}
	}
}

// Anthropic surface accepts Bearer auth in addition to x-api-key.
func TestNew_Anthropic_BearerAuth_SucceedsWithValidKey(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"content":"hello from kiro"}`)
	}))
	defer upstream.Close()

	cfg := config.Config{
		ProxyAPIKey:                    "test-key",
		StreamingReadTimeout:           2,
		FirstTokenTimeout:              0.1,
		FirstTokenMaxRetries:           2,
		FakeReasoningHandling:          "remove",
		FakeReasoningMaxTokens:         256,
		ToolDescriptionMaxLength:       10000,
		TruncationRecovery:             true,
		FakeReasoningInitialBufferSize: 20,
	}
	authMgr := newTestAuthManagerWithUpstream(t, upstream.URL)
	router := New(cfg, authMgr, newTestResolver(), &http.Client{Timeout: 2 * time.Second})

	body := `{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key") // Bearer, not x-api-key
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"type":"message"`) {
		t.Fatalf("unexpected response body: %q", rr.Body.String())
	}
}

// Missing or wrong Anthropic client auth returns Anthropic protocol-correct auth errors.
func TestNew_Anthropic_AuthRejection_ErrorEnvelope(t *testing.T) {
	r := newRouter(t)

	for _, tc := range []struct {
		name string
		auth string // value for x-api-key; empty means no auth header at all
		hdr  string // which header to set ("x-api-key" or "Authorization")
	}{
		{"no_auth", "", ""},
		{"wrong_x_api_key", "wrong-key", "x-api-key"},
		{"wrong_bearer", "Bearer wrong-key", "Authorization"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("{}"))
			if tc.hdr != "" {
				req.Header.Set(tc.hdr, tc.auth)
			}
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
			}

			var body map[string]any
			if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}

			// Anthropic error envelope: top-level type:"error" + nested error object.
			if body["type"] != "error" {
				t.Fatalf("top-level type = %v, want error", body["type"])
			}

			errObj, ok := body["error"].(map[string]any)
			if !ok {
				t.Fatal("response missing 'error' object")
			}
			if errObj["type"] != "authentication_error" {
				t.Fatalf("error.type = %v, want authentication_error", errObj["type"])
			}
			if errObj["message"] == nil || errObj["message"] == "" {
				t.Fatal("error.message is empty or missing")
			}
		})
	}
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
