package e2e

import (
	"encoding/json"
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

// crossAreaPNGBase64 is a valid 10×10 solid red PNG for vision tests.
const crossAreaPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAoAAAAKCAIAAAACUFjqAAAAEklEQVR4nGP4z8CAB+GTG8HSALfKY52fTcuYAAAAAElFTkSuQmCC"

func newCrossAreaAuthManager(t *testing.T, apiHost, qHost string) *auth.KiroAuthManager {
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

func newCrossAreaResolver() *model.Resolver {
	cache := model.NewInfoCache(time.Hour)
	models := make([]model.Info, 0, len(config.FallbackModels))
	for _, id := range config.FallbackModels {
		models = append(models, model.Info{ModelID: id, MaxInputTokens: config.DefaultMaxInputTokens})
	}
	cache.Update(models)
	return model.NewResolver(cache, config.HiddenModels, config.ModelAliases, config.HiddenFromList)
}

// TestCrossAreaMatrix exercises the full local validation matrix against one
// mock-backed server instance: health, models, OpenAI non-stream, OpenAI
// stream, Anthropic non-stream, Anthropic stream, negative client auth, tool
// use, and vision — proving all nine items work in a single process lifetime.
func TestCrossAreaMatrix(t *testing.T) {
	// Smart Kiro mock: routes by request content.
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)

		w.WriteHeader(http.StatusOK)

		if strings.Contains(bodyStr, `"tools"`) {
			_, _ = fmt.Fprint(w, `{"content":"Let me check."}{"name":"get_weather","toolUseId":"call_abc","input":"{\"city\":\"NYC\"}","stop":true}`)
			return
		}

		if strings.Contains(bodyStr, `"images"`) {
			_, _ = fmt.Fprint(w, `{"content":"I see a small red square in the image."}`)
			return
		}

		_, _ = fmt.Fprint(w, `{"content":"hello from kiro"}`)
	}))
	defer kiro.Close()

	cfg := config.Config{
		ProxyAPIKey:                    "e2e-cross-key",
		StreamingReadTimeout:           2,
		FirstTokenTimeout:              0.1,
		FirstTokenMaxRetries:           2,
		FakeReasoningHandling:          "remove",
		FakeReasoningMaxTokens:         256,
		ToolDescriptionMaxLength:       10000,
		TruncationRecovery:             true,
		FakeReasoningInitialBufferSize: 20,
	}

	authMgr := newCrossAreaAuthManager(t, kiro.URL, kiro.URL)
	resolver := newCrossAreaResolver()
	router := server.New(cfg, authMgr, resolver, &http.Client{Timeout: 2 * time.Second})
	gateway := httptest.NewServer(router)
	defer gateway.Close()

	client := &http.Client{Timeout: 5 * time.Second}

	t.Run("health", func(t *testing.T) {
		crossAreaAssertHealth(t, client, gateway.URL)
	})
	t.Run("models", func(t *testing.T) {
		crossAreaAssertModels(t, client, gateway.URL)
	})
	t.Run("openai_non_stream", func(t *testing.T) {
		crossAreaAssertOpenAI(t, client, gateway.URL, false)
	})
	t.Run("openai_stream", func(t *testing.T) {
		crossAreaAssertOpenAI(t, client, gateway.URL, true)
	})
	t.Run("anthropic_non_stream", func(t *testing.T) {
		crossAreaAssertAnthropic(t, client, gateway.URL, false)
	})
	t.Run("anthropic_stream", func(t *testing.T) {
		crossAreaAssertAnthropic(t, client, gateway.URL, true)
	})
	t.Run("negative_auth_openai", func(t *testing.T) {
		crossAreaAssertNegativeAuth(t, client, gateway.URL, "openai")
	})
	t.Run("negative_auth_anthropic", func(t *testing.T) {
		crossAreaAssertNegativeAuth(t, client, gateway.URL, "anthropic")
	})
	t.Run("tool_use", func(t *testing.T) {
		crossAreaAssertToolUse(t, client, gateway.URL)
	})
	t.Run("vision", func(t *testing.T) {
		crossAreaAssertVision(t, client, gateway.URL)
	})
}

// --- cross-area matrix helpers ---------------------------------------------

func crossAreaAssertHealth(t *testing.T, client *http.Client, gatewayURL string) {
	t.Helper()
	resp, err := client.Get(gatewayURL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func crossAreaAssertModels(t *testing.T, client *http.Client, gatewayURL string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, gatewayURL+"/v1/models", nil)
	req.Header.Set("Authorization", "Bearer e2e-cross-key")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["object"] != "list" {
		t.Fatalf("object = %v, want list", body["object"])
	}
	data, ok := body["data"].([]any)
	if !ok || len(data) == 0 {
		t.Fatal("data must be a non-empty array")
	}
	ids := make(map[string]bool)
	for _, item := range data {
		m, _ := item.(map[string]any)
		id, _ := m["id"].(string)
		ids[id] = true
	}
	if !ids["auto-kiro"] {
		t.Fatalf("missing auto-kiro; got %v", ids)
	}
	if !ids["claude-3.7-sonnet"] {
		t.Fatalf("missing claude-3.7-sonnet; got %v", ids)
	}
}

func crossAreaAssertOpenAI(t *testing.T, client *http.Client, gatewayURL string, stream bool) {
	t.Helper()
	body := fmt.Sprintf(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hi"}],"stream":%v}`, stream)
	req, _ := http.NewRequest(http.MethodPost, gatewayURL+"/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer e2e-cross-key")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, http.StatusOK, respBody)
	}
	if stream {
		if !strings.Contains(string(respBody), "data: [DONE]") {
			t.Fatalf("stream missing [DONE]: %s", respBody)
		}
	} else {
		if !strings.Contains(string(respBody), `"choices"`) {
			t.Fatalf("unexpected body: %s", respBody)
		}
	}
}

func crossAreaAssertAnthropic(t *testing.T, client *http.Client, gatewayURL string, stream bool) {
	t.Helper()
	body := fmt.Sprintf(`{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"stream":%v}`, stream)
	req, _ := http.NewRequest(http.MethodPost, gatewayURL+"/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "e2e-cross-key")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, http.StatusOK, respBody)
	}
	if stream {
		if !strings.Contains(string(respBody), "event: message_stop") {
			t.Fatalf("stream missing message_stop: %s", respBody)
		}
	} else {
		if !strings.Contains(string(respBody), `"type":"message"`) {
			t.Fatalf("unexpected body: %s", respBody)
		}
	}
}

func crossAreaAssertNegativeAuth(t *testing.T, client *http.Client, gatewayURL, surface string) {
	t.Helper()
	var req *http.Request
	if surface == "openai" {
		req, _ = http.NewRequest(http.MethodGet, gatewayURL+"/v1/models", nil)
		req.Header.Set("Authorization", "Bearer wrong-key")
	} else {
		req, _ = http.NewRequest(http.MethodPost, gatewayURL+"/v1/messages", strings.NewReader("{}"))
		req.Header.Set("x-api-key", "wrong-key")
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func crossAreaAssertToolUse(t *testing.T, client *http.Client, gatewayURL string) {
	t.Helper()
	body := `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"weather in NYC?"}],"stream":false,"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}]}`
	req, _ := http.NewRequest(http.MethodPost, gatewayURL+"/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer e2e-cross-key")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, http.StatusOK, respBody)
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	choices, _ := result["choices"].([]any)
	if len(choices) == 0 {
		t.Fatal("choices is empty")
	}
	choice := choices[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Fatalf("finish_reason = %v, want tool_calls", choice["finish_reason"])
	}
}

func crossAreaAssertVision(t *testing.T, client *http.Client, gatewayURL string) {
	t.Helper()
	body := `{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"Describe the dominant color of this image in one word."},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + crossAreaPNGBase64 + `"}}]}],"stream":false}`
	req, _ := http.NewRequest(http.MethodPost, gatewayURL+"/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "e2e-cross-key")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, http.StatusOK, respBody)
	}
	// The image is a solid red 10×10 PNG. A generic envelope that ignores
	// the image will not mention "red". Require image-grounded content.
	lower := strings.ToLower(string(respBody))
	if !strings.Contains(lower, "red") {
		t.Fatalf("vision response should mention 'red' (image-grounded); got: %s", respBody)
	}
}
