package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestOpenAIHandler_Models(t *testing.T) {
	h := NewOpenAIHandler(
		newTestAuthManager(t, "http://127.0.0.1:1", "http://127.0.0.1:1"),
		newTestResolver("claude-sonnet-4", "claude-haiku-4.5"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rr := httptest.NewRecorder()
	h.Models(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got["object"] != "list" {
		t.Fatalf("object = %v, want list", got["object"])
	}
	data, ok := got["data"].([]any)
	if !ok || len(data) != 2 {
		t.Fatalf("data length = %v, want 2", len(data))
	}
}

func TestOpenAIHandler_ChatCompletions_InvalidBody(t *testing.T) {
	h := NewOpenAIHandler(
		newTestAuthManager(t, "http://127.0.0.1:1", "http://127.0.0.1:1"),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{"))
	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rr.Body.String(), "invalid request body") {
		t.Fatalf("expected invalid request body error, got %q", rr.Body.String())
	}
}

func TestOpenAIHandler_ChatCompletions_NonStream_Success(t *testing.T) {
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/generateAssistantResponse" {
			t.Fatalf("path = %q, want /generateAssistantResponse", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"content":"hello from kiro"}`)
	}))
	defer kiro.Close()

	h := NewOpenAIHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	body := `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	choices, ok := resp["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Fatalf("choices missing in response: %v", resp)
	}
}

func TestOpenAIHandler_ChatCompletions_Stream_Success(t *testing.T) {
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"content":"hello stream"}`)
	}))
	defer kiro.Close()

	h := NewOpenAIHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	body := `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := rr.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	resp := rr.Body.String()
	if !strings.Contains(resp, "chat.completion.chunk") {
		t.Fatalf("stream output missing chunk payload: %q", resp)
	}
	if !strings.Contains(resp, "data: [DONE]") {
		t.Fatalf("stream output missing [DONE]: %q", resp)
	}
}

func TestOpenAIHandler_ChatCompletions_UpstreamJSONError(t *testing.T) {
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"message":"ctx too long","reason":"CONTENT_LENGTH_EXCEEDS_THRESHOLD"}`)
	}))
	defer kiro.Close()

	h := NewOpenAIHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	body := `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rr.Body.String(), "Model context limit reached") {
		t.Fatalf("unexpected error body: %q", rr.Body.String())
	}
}

func TestOpenAIHandler_ChatCompletions_UpstreamTextError(t *testing.T) {
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = fmt.Fprint(w, "boom")
	}))
	defer kiro.Close()

	h := NewOpenAIHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	body := `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusTeapot)
	}
	if !strings.Contains(rr.Body.String(), "boom") {
		t.Fatalf("unexpected error body: %q", rr.Body.String())
	}
}

func TestOpenAIHandler_ChatCompletions_FirstTokenRetry_Succeeds(t *testing.T) {
	var calls atomic.Int32
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"content":"retry ok"}`)
	}))
	defer kiro.Close()

	cfg := testHandlerConfig()
	cfg.FirstTokenMaxRetries = 2

	h := NewOpenAIHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		cfg,
	)

	body := `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestOpenAIHandler_ChatCompletions_FirstTokenRetry_Exhausted(t *testing.T) {
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer kiro.Close()

	cfg := testHandlerConfig()
	cfg.FirstTokenMaxRetries = 2

	h := NewOpenAIHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		cfg,
	)

	body := `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadGateway)
	}
	if !strings.Contains(rr.Body.String(), "all 2 first-token retry attempts failed") {
		t.Fatalf("unexpected error body: %q", rr.Body.String())
	}
}
