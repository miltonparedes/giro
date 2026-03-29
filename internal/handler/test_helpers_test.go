package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miltonparedes/giro/internal/auth"
	"github.com/miltonparedes/giro/internal/config"
	"github.com/miltonparedes/giro/internal/model"
)

// testPNGBase64 is a valid 10×10 solid red PNG image encoded as base64.
// This is the minimum viable fixture for vision tests: small enough to keep
// test payloads readable but large enough that the real Kiro upstream accepts
// it without "Improperly formed request" errors (1×1 or truncated PNGs are
// silently dropped or rejected upstream even though they pass mock coverage).
const testPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAoAAAAKCAIAAAACUFjqAAAAEklEQVR4nGP4z8CAB+GTG8HSALfKY52fTcuYAAAAAElFTkSuQmCC"

// testPNGDataURL is testPNGBase64 wrapped in a data URL for OpenAI image_url blocks.
const testPNGDataURL = "data:image/png;base64," + testPNGBase64

func newTestAuthManager(t *testing.T, apiHost, qHost string) *auth.KiroAuthManager {
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

func newTestResolver(ids ...string) *model.Resolver {
	cache := model.NewInfoCache(time.Hour)
	models := make([]model.Info, 0, len(ids))
	for _, id := range ids {
		models = append(models, model.Info{ModelID: id, MaxInputTokens: config.DefaultMaxInputTokens})
	}
	cache.Update(models)
	return model.NewResolver(cache, map[string]string{}, map[string]string{}, nil)
}

func testHandlerConfig() config.Config {
	return config.Config{
		StreamingReadTimeout:           2,
		FirstTokenTimeout:              0.05,
		FirstTokenMaxRetries:           2,
		FakeReasoning:                  false,
		FakeReasoningHandling:          "remove",
		FakeReasoningMaxTokens:         256,
		ToolDescriptionMaxLength:       10000,
		TruncationRecovery:             true,
		FakeReasoningInitialBufferSize: 20,
	}
}

func newTestHTTPClient() *http.Client {
	return &http.Client{Timeout: 2 * time.Second}
}

// assertOpenAICompletionShape verifies the full shape of a non-streaming
// OpenAI chat-completion response and returns the parsed body.
func assertOpenAICompletionShape(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["object"] != "chat.completion" {
		t.Fatalf("object = %v, want chat.completion", resp["object"])
	}
	id, _ := resp["id"].(string)
	if !strings.HasPrefix(id, "chatcmpl-") {
		t.Fatalf("id = %q, want chatcmpl- prefix", id)
	}
	if resp["model"] == nil || resp["model"] == "" {
		t.Fatal("model is missing")
	}

	choices, ok := resp["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Fatal("choices is empty or missing")
	}

	choice := choices[0].(map[string]any)
	message, ok := choice["message"].(map[string]any)
	if !ok {
		t.Fatal("choices[0].message is missing")
	}
	if message["role"] != "assistant" {
		t.Fatalf("message.role = %v, want assistant", message["role"])
	}

	assertOpenAIUsage(t, resp)

	return resp
}

// assertOpenAIUsage checks that the usage object is present with all required fields.
func assertOpenAIUsage(t *testing.T, resp map[string]any) {
	t.Helper()
	usage, ok := resp["usage"].(map[string]any)
	if !ok {
		t.Fatal("usage object is missing")
	}
	for _, field := range []string{"prompt_tokens", "completion_tokens", "total_tokens"} {
		if _, ok := usage[field]; !ok {
			t.Fatalf("usage.%s is missing", field)
		}
	}
}

// sseChunk is a parsed SSE data event from an OpenAI stream response.
type sseChunk struct {
	Raw     string
	Payload map[string]any
	IsDone  bool
}

// parseSSEChunks splits a raw SSE response into structured chunks.
func parseSSEChunks(t *testing.T, output string) []sseChunk {
	t.Helper()
	var chunks []sseChunk
	for _, line := range strings.Split(output, "\n\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "data: [DONE]" {
			chunks = append(chunks, sseChunk{Raw: line, IsDone: true})
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			t.Fatalf("unexpected SSE line format: %q", line)
		}
		payload := strings.TrimPrefix(line, "data: ")
		var m map[string]any
		if err := json.Unmarshal([]byte(payload), &m); err != nil {
			t.Fatalf("parse SSE chunk: %v (raw=%q)", err, payload)
		}
		chunks = append(chunks, sseChunk{Raw: line, Payload: m})
	}
	return chunks
}

// sseChunkDelta extracts the delta from the first choice of a parsed SSE chunk.
func sseChunkDelta(chunk sseChunk) map[string]any {
	cs, _ := chunk.Payload["choices"].([]any)
	if len(cs) == 0 {
		return nil
	}
	delta, _ := cs[0].(map[string]any)["delta"].(map[string]any)
	return delta
}

// sseChunkFinishReason returns the finish_reason from the first choice of a chunk.
func sseChunkFinishReason(chunk sseChunk) string {
	cs, _ := chunk.Payload["choices"].([]any)
	if len(cs) == 0 {
		return ""
	}
	fr, _ := cs[0].(map[string]any)["finish_reason"].(string)
	return fr
}
