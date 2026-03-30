//go:build e2e_mock

package e2e

import (
	"fmt"
	"io"
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
	"github.com/miltonparedes/giro/internal/server"
)

// testPNGBase64 is a valid 10×10 solid red PNG for vision tests.
// Truncated or 1×1 images cause "Improperly formed request" on the real Kiro upstream.
const testPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAoAAAAKCAIAAAACUFjqAAAAEklEQVR4nGP4z8CAB+GTG8HSALfKY52fTcuYAAAAAElFTkSuQmCC"

func newMockAuthManager(t *testing.T, apiHost, qHost string) *auth.KiroAuthManager {
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

func newMockResolver() *model.Resolver {
	cache := model.NewInfoCache(time.Hour)
	cache.Update([]model.Info{{ModelID: "claude-sonnet-4", MaxInputTokens: config.DefaultMaxInputTokens}})
	return model.NewResolver(cache, map[string]string{}, map[string]string{}, nil)
}

func TestE2EMock_OpenAIAndAnthropic(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/generateAssistantResponse" {
			t.Fatalf("path = %q, want /generateAssistantResponse", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "" {
			t.Fatal("missing Authorization header to upstream")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"content":"hello from mocked kiro"}`)
	}))
	defer upstream.Close()

	cfg := config.Config{
		ProxyAPIKey:                    "e2e-key",
		StreamingReadTimeout:           2,
		FirstTokenTimeout:              0.1,
		FirstTokenMaxRetries:           2,
		FakeReasoning:                  false,
		FakeReasoningHandling:          "remove",
		FakeReasoningMaxTokens:         256,
		ToolDescriptionMaxLength:       10000,
		TruncationRecovery:             true,
		FakeReasoningInitialBufferSize: 20,
	}

	router := server.New(cfg, newMockAuthManager(t, upstream.URL, upstream.URL), newMockResolver(), &http.Client{Timeout: 2 * time.Second})
	gateway := httptest.NewServer(router)
	defer gateway.Close()

	client := &http.Client{Timeout: 5 * time.Second}

	t.Run("openai non-stream", func(t *testing.T) {
		body := `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}],"stream":false}`
		req, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(body))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer e2e-key")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(respBody))
		}
		if !strings.Contains(string(respBody), `"choices"`) {
			t.Fatalf("unexpected body: %s", string(respBody))
		}
	})

	t.Run("openai stream", func(t *testing.T) {
		body := `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}],"stream":true}`
		req, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(body))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer e2e-key")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(respBody))
		}
		if !strings.Contains(string(respBody), "data: [DONE]") {
			t.Fatalf("missing [DONE] marker in stream body: %s", string(respBody))
		}
	})

	t.Run("anthropic non-stream", func(t *testing.T) {
		body := `{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"stream":false}`
		req, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/messages", strings.NewReader(body))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", "e2e-key")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(respBody))
		}
		if !strings.Contains(string(respBody), `"type":"message"`) {
			t.Fatalf("unexpected body: %s", string(respBody))
		}
	})

	t.Run("anthropic stream", func(t *testing.T) {
		body := `{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"stream":true}`
		req, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/messages", strings.NewReader(body))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", "e2e-key")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(respBody))
		}
		if !strings.Contains(string(respBody), "event: message_stop") {
			t.Fatalf("missing message_stop event in stream body: %s", string(respBody))
		}
	})

	// OpenAI base64 vision via e2e mock (valid 10x10 PNG).
	t.Run("openai vision non-stream", func(t *testing.T) {
		body := `{
			"model": "claude-sonnet-4",
			"messages": [{
				"role": "user",
				"content": [
					{"type": "text", "text": "What is in this image?"},
					{"type": "image_url", "image_url": {"url": "data:image/png;base64,` + testPNGBase64 + `"}}
				]
			}],
			"stream": false
		}`
		req, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(body))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer e2e-key")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(respBody))
		}
		if !strings.Contains(string(respBody), `"choices"`) {
			t.Fatalf("unexpected body: %s", string(respBody))
		}
	})

	// Anthropic base64 vision via e2e mock (valid 10x10 PNG).
	t.Run("anthropic vision non-stream", func(t *testing.T) {
		body := `{
			"model": "claude-sonnet-4",
			"max_tokens": 64,
			"messages": [{
				"role": "user",
				"content": [
					{"type": "text", "text": "What is in this image?"},
					{
						"type": "image",
						"source": {
							"type": "base64",
							"media_type": "image/png",
							"data": "` + testPNGBase64 + `"
						}
					}
				]
			}],
			"stream": false
		}`
		req, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/messages", strings.NewReader(body))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", "e2e-key")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(respBody))
		}
		if !strings.Contains(string(respBody), `"type":"message"`) {
			t.Fatalf("unexpected body: %s", string(respBody))
		}
	})
}
