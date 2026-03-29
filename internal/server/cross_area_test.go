package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/miltonparedes/giro/internal/config"
	"github.com/miltonparedes/giro/internal/model"
)

// crossAreaPNGBase64 is a valid 10×10 solid red PNG for vision tests.
const crossAreaPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAoAAAAKCAIAAAACUFjqAAAAEklEQVR4nGP4z8CAB+GTG8HSALfKY52fTcuYAAAAAElFTkSuQmCC"

// newProductionLikeResolver builds a resolver using the real fallback models,
// hidden models, aliases, and hidden-from-list from config — mirroring what a
// production giro process exposes through /v1/models.
func newProductionLikeResolver() *model.Resolver {
	cache := model.NewInfoCache(time.Hour)
	models := make([]model.Info, 0, len(config.FallbackModels))
	for _, id := range config.FallbackModels {
		models = append(models, model.Info{ModelID: id, MaxInputTokens: config.DefaultMaxInputTokens})
	}
	cache.Update(models)
	return model.NewResolver(cache, config.HiddenModels, config.ModelAliases, config.HiddenFromList)
}

// newCrossAreaRouter creates a chi router with production-like model configuration,
// a mock auth manager pointed at the given upstream, and PROXY_API_KEY enabled.
func newCrossAreaRouter(t *testing.T, upstreamURL string) http.Handler {
	t.Helper()

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

	return New(cfg, newTestAuthManagerWithUpstream(t, upstreamURL), newProductionLikeResolver(), &http.Client{Timeout: 2 * time.Second})
}

// smartKiroMock returns an http.Handler that routes Kiro responses based on
// request content: tool calls when tools are present, image-grounded answers
// when images are present, and plain text otherwise.
func smartKiroMock() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})
}

// --- VAL-CROSS-001 ---------------------------------------------------------

// TestCrossArea_HealthPublicModelsAuthenticated verifies that /health remains
// public while /v1/models remains authenticated and exposes key user-visible
// model entries including auto-kiro and claude-3.7-sonnet, all from the same
// server instance.
func TestCrossArea_HealthPublicModelsAuthenticated(t *testing.T) {
	kiro := httptest.NewServer(smartKiroMock())
	defer kiro.Close()

	gateway := httptest.NewServer(newCrossAreaRouter(t, kiro.URL))
	defer gateway.Close()

	client := &http.Client{Timeout: 5 * time.Second}

	// /health must be reachable without client auth.
	t.Run("health_no_auth", func(t *testing.T) {
		resp, err := client.Get(gateway.URL + "/health")
		if err != nil {
			t.Fatalf("GET /health: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /health status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	// /v1/models without auth must return 401.
	t.Run("models_no_auth_rejected", func(t *testing.T) {
		resp, err := client.Get(gateway.URL + "/v1/models")
		if err != nil {
			t.Fatalf("GET /v1/models: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("GET /v1/models (no auth) status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}
	})

	// /v1/models with auth must return 200 with auto-kiro and claude-3.7-sonnet.
	t.Run("models_authenticated_with_key_entries", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, gateway.URL+"/v1/models", nil)
		req.Header.Set("Authorization", "Bearer test-key")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET /v1/models: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /v1/models status = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
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
			t.Fatalf("model list missing auto-kiro; got %v", ids)
		}
		if !ids["claude-3.7-sonnet"] {
			t.Fatalf("model list missing claude-3.7-sonnet; got %v", ids)
		}
	})
}

// --- VAL-CROSS-002 ---------------------------------------------------------

// TestCrossArea_OneSourceBothProtocols verifies that one giro server instance
// backed by one resolved credential source can serve both a successful OpenAI
// request and a successful Anthropic request without restart.
func TestCrossArea_OneSourceBothProtocols(t *testing.T) {
	kiro := httptest.NewServer(smartKiroMock())
	defer kiro.Close()

	gateway := httptest.NewServer(newCrossAreaRouter(t, kiro.URL))
	defer gateway.Close()

	client := &http.Client{Timeout: 5 * time.Second}

	// OpenAI non-stream succeeds.
	t.Run("openai_request", func(t *testing.T) {
		body := `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hi"}],"stream":false}`
		req, _ := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-key")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("OpenAI request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			t.Fatalf("OpenAI status = %d, want %d; body=%s", resp.StatusCode, http.StatusOK, respBody)
		}
	})

	// Anthropic non-stream succeeds on the same server.
	t.Run("anthropic_request", func(t *testing.T) {
		body := `{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"stream":false}`
		req, _ := http.NewRequest(http.MethodPost, gateway.URL+"/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", "test-key")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Anthropic request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			t.Fatalf("Anthropic status = %d, want %d; body=%s", resp.StatusCode, http.StatusOK, respBody)
		}
	})
}

// --- VAL-CROSS-003 ---------------------------------------------------------

// TestCrossArea_NegativeAuthBothProtocols verifies that invalid client auth is
// rejected before any upstream response on both API surfaces, and each surface
// keeps its own protocol-correct error contract.
func TestCrossArea_NegativeAuthBothProtocols(t *testing.T) {
	kiro := httptest.NewServer(smartKiroMock())
	defer kiro.Close()

	gateway := httptest.NewServer(newCrossAreaRouter(t, kiro.URL))
	defer gateway.Close()

	client := &http.Client{Timeout: 5 * time.Second}

	// OpenAI surface: invalid auth returns OpenAI error envelope.
	t.Run("openai_invalid_auth", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer wrong-key")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}

		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}

		errObj, ok := body["error"].(map[string]any)
		if !ok {
			t.Fatal("OpenAI response missing 'error' object")
		}
		if errObj["type"] != "authentication_error" {
			t.Fatalf("error.type = %v, want authentication_error", errObj["type"])
		}
	})

	// Anthropic surface: invalid auth returns Anthropic error envelope.
	t.Run("anthropic_invalid_auth", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, gateway.URL+"/v1/messages", strings.NewReader("{}"))
		req.Header.Set("x-api-key", "wrong-key")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}

		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}

		// Anthropic envelope: top-level type:"error" + nested error object.
		if body["type"] != "error" {
			t.Fatalf("top-level type = %v, want error", body["type"])
		}
		errObj, ok := body["error"].(map[string]any)
		if !ok {
			t.Fatal("Anthropic response missing 'error' object")
		}
		if errObj["type"] != "authentication_error" {
			t.Fatalf("error.type = %v, want authentication_error", errObj["type"])
		}
	})
}

// --- VAL-CROSS-004 ---------------------------------------------------------

// TestCrossArea_AllFourModes verifies that one giro server instance serves
// OpenAI non-stream, OpenAI stream, Anthropic non-stream, and Anthropic stream
// requests successfully without restart or reconfiguration.
func TestCrossArea_AllFourModes(t *testing.T) {
	kiro := httptest.NewServer(smartKiroMock())
	defer kiro.Close()

	gateway := httptest.NewServer(newCrossAreaRouter(t, kiro.URL))
	defer gateway.Close()

	client := &http.Client{Timeout: 5 * time.Second}

	t.Run("openai_non_stream", func(t *testing.T) {
		body := `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hi"}],"stream":false}`
		req, _ := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-key")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, http.StatusOK, respBody)
		}
		if !strings.Contains(string(respBody), `"choices"`) {
			t.Fatalf("unexpected response: %s", respBody)
		}
	})

	t.Run("openai_stream", func(t *testing.T) {
		body := `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hi"}],"stream":true}`
		req, _ := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-key")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, http.StatusOK, respBody)
		}
		if resp.Header.Get("Content-Type") != "text/event-stream" {
			t.Fatalf("Content-Type = %q, want text/event-stream", resp.Header.Get("Content-Type"))
		}
		if !strings.Contains(string(respBody), "data: [DONE]") {
			t.Fatalf("stream missing [DONE]: %s", respBody)
		}
	})

	t.Run("anthropic_non_stream", func(t *testing.T) {
		body := `{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"stream":false}`
		req, _ := http.NewRequest(http.MethodPost, gateway.URL+"/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", "test-key")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, http.StatusOK, respBody)
		}
		if !strings.Contains(string(respBody), `"type":"message"`) {
			t.Fatalf("unexpected response: %s", respBody)
		}
	})

	t.Run("anthropic_stream", func(t *testing.T) {
		body := `{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"stream":true}`
		req, _ := http.NewRequest(http.MethodPost, gateway.URL+"/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", "test-key")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, http.StatusOK, respBody)
		}
		if !strings.Contains(string(respBody), "event: message_start") {
			t.Fatalf("stream missing message_start: %s", respBody)
		}
		if !strings.Contains(string(respBody), "event: message_stop") {
			t.Fatalf("stream missing message_stop: %s", respBody)
		}
	})
}

// --- VAL-CROSS-005 ---------------------------------------------------------

// TestCrossArea_AdvancedMatrix verifies that one giro server instance can
// execute negative client auth, at least one tool-use flow, and at least one
// base64 vision flow in the same validation session.
func TestCrossArea_AdvancedMatrix(t *testing.T) {
	kiro := httptest.NewServer(smartKiroMock())
	defer kiro.Close()

	gateway := httptest.NewServer(newCrossAreaRouter(t, kiro.URL))
	defer gateway.Close()

	client := &http.Client{Timeout: 5 * time.Second}

	t.Run("negative_auth_openai", func(t *testing.T) {
		assertCrossAreaNegativeAuth(t, client, gateway.URL, "openai")
	})
	t.Run("negative_auth_anthropic", func(t *testing.T) {
		assertCrossAreaNegativeAuth(t, client, gateway.URL, "anthropic")
	})
	t.Run("tool_use_openai", func(t *testing.T) {
		assertCrossAreaToolUse(t, client, gateway.URL, "openai")
	})
	t.Run("tool_use_anthropic", func(t *testing.T) {
		assertCrossAreaToolUse(t, client, gateway.URL, "anthropic")
	})
	t.Run("vision_openai", func(t *testing.T) {
		assertCrossAreaVision(t, client, gateway.URL, "openai")
	})
	t.Run("vision_anthropic", func(t *testing.T) {
		assertCrossAreaVision(t, client, gateway.URL, "anthropic")
	})
}

// --- cross-area assertion helpers ------------------------------------------

func assertCrossAreaNegativeAuth(t *testing.T, client *http.Client, gatewayURL, surface string) {
	t.Helper()

	var req *http.Request
	if surface == "openai" {
		req, _ = http.NewRequest(http.MethodGet, gatewayURL+"/v1/models", nil)
		req.Header.Set("Authorization", "Bearer bad-key")
	} else {
		req, _ = http.NewRequest(http.MethodPost, gatewayURL+"/v1/messages", strings.NewReader("{}"))
		req.Header.Set("x-api-key", "bad-key")
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

func assertCrossAreaToolUse(t *testing.T, client *http.Client, gatewayURL, surface string) {
	t.Helper()

	var req *http.Request
	if surface == "openai" {
		body := `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"weather in NYC?"}],"stream":false,"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}]}`
		req, _ = http.NewRequest(http.MethodPost, gatewayURL+"/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-key")
	} else {
		body := `{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"weather?"}],"stream":false,"tools":[{"name":"get_weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}}}}]}`
		req, _ = http.NewRequest(http.MethodPost, gatewayURL+"/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", "test-key")
	}

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

	if surface == "openai" {
		choices, _ := result["choices"].([]any)
		if len(choices) == 0 {
			t.Fatal("choices is empty")
		}
		choice := choices[0].(map[string]any)
		if choice["finish_reason"] != "tool_calls" {
			t.Fatalf("finish_reason = %v, want tool_calls", choice["finish_reason"])
		}
		message, _ := choice["message"].(map[string]any)
		if message["tool_calls"] == nil {
			t.Fatal("message.tool_calls is missing")
		}
	} else if result["stop_reason"] != "tool_use" {
		t.Fatalf("stop_reason = %v, want tool_use", result["stop_reason"])
	}
}

func assertCrossAreaVision(t *testing.T, client *http.Client, gatewayURL, surface string) {
	t.Helper()

	var req *http.Request
	if surface == "openai" {
		body := `{"model":"claude-sonnet-4","messages":[{"role":"user","content":[{"type":"text","text":"What is in this image?"},{"type":"image_url","image_url":{"url":"data:image/png;base64,` + crossAreaPNGBase64 + `"}}]}],"stream":false}`
		req, _ = http.NewRequest(http.MethodPost, gatewayURL+"/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-key")
	} else {
		body := `{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"What is in this image?"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + crossAreaPNGBase64 + `"}}]}],"stream":false}`
		req, _ = http.NewRequest(http.MethodPost, gatewayURL+"/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", "test-key")
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, http.StatusOK, respBody)
	}
	if !strings.Contains(string(respBody), "image") {
		t.Fatalf("vision response should contain image-grounded content: %s", respBody)
	}
}
