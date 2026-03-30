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

// --- OpenAI assertion-targeted tests ------------------------------------

// A model advertised by /v1/models is the exact model forwarded to Kiro.
func TestOpenAIHandler_AdvertisedModelUsableInCompletions(t *testing.T) {
	var forwardedModel string
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the outbound Kiro request body to verify model forwarding.
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
			convState, _ := payload["conversationState"].(map[string]any)
			cm, _ := convState["currentMessage"].(map[string]any)
			ui, _ := cm["userInputMessage"].(map[string]any)
			forwardedModel, _ = ui["modelId"].(string)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"content":"works"}`)
	}))
	defer kiro.Close()

	resolver := newTestResolver("claude-sonnet-4", "claude-haiku-4.5")
	h := NewOpenAIHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		resolver,
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	// Step 1: get advertised models from the Models endpoint.
	modelsReq := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	modelsRR := httptest.NewRecorder()
	h.Models(modelsRR, modelsReq)

	if modelsRR.Code != http.StatusOK {
		t.Fatalf("models status = %d, want %d", modelsRR.Code, http.StatusOK)
	}

	var modelList struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(modelsRR.Body.Bytes(), &modelList); err != nil {
		t.Fatalf("decode models: %v", err)
	}
	if len(modelList.Data) == 0 {
		t.Fatal("no models advertised")
	}

	// Step 2: use the first advertised model in a chat completion.
	advertisedModel := modelList.Data[0].ID
	body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}],"stream":false}`, advertisedModel)
	compReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	compRR := httptest.NewRecorder()
	h.ChatCompletions(compRR, compReq)

	if compRR.Code != http.StatusOK {
		t.Fatalf("completions status = %d, want %d; body=%s", compRR.Code, http.StatusOK, compRR.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(compRR.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode completions: %v", err)
	}
	choices, _ := resp["choices"].([]any)
	if len(choices) == 0 {
		t.Fatal("completions response has no choices")
	}

	// Step 3: verify the advertised model was forwarded to Kiro in the outbound payload.
	if forwardedModel == "" {
		t.Fatal("kiro mock did not receive a modelId in the outbound request payload")
	}
	if forwardedModel != advertisedModel {
		t.Fatalf("kiro payload modelId = %q, want %q (the advertised model)", forwardedModel, advertisedModel)
	}
}

// Malformed JSON returns a structured OpenAI-surface error envelope.
func TestOpenAIHandler_ChatCompletions_MalformedJSON_ErrorEnvelope(t *testing.T) {
	h := NewOpenAIHandler(
		newTestAuthManager(t, "http://127.0.0.1:1", "http://127.0.0.1:1"),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{invalid"))
	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("response missing 'error' object")
	}
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "invalid request body") {
		t.Fatalf("error.message = %q, want it to mention invalid request body", msg)
	}
	if errObj["type"] == nil || errObj["type"] == "" {
		t.Fatal("error.type is missing")
	}
	if errObj["code"] == nil {
		t.Fatal("error.code is missing")
	}
}

// Non-stream completions return a full OpenAI chat-completion JSON shape.
func TestOpenAIHandler_ChatCompletions_NonStream_FullResponseShape(t *testing.T) {
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	resp := assertOpenAICompletionShape(t, rr.Body.Bytes())

	choice := resp["choices"].([]any)[0].(map[string]any)
	content, _ := choice["message"].(map[string]any)["content"].(string)
	if content == "" {
		t.Fatal("message.content is empty")
	}

	fr, _ := choice["finish_reason"].(string)
	if fr != "stop" {
		t.Fatalf("finish_reason = %q, want stop", fr)
	}
}

// Streaming completions use OpenAI SSE framing and terminate with [DONE].
func TestOpenAIHandler_ChatCompletions_Stream_FullFraming(t *testing.T) {
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"content":"streamed answer"}`)
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

	chunks := parseSSEChunks(t, rr.Body.String())
	var foundRole, foundContent, foundFinish, foundDone bool
	for _, c := range chunks {
		if c.IsDone {
			foundDone = true
			continue
		}
		if c.Payload["object"] != "chat.completion.chunk" {
			t.Fatalf("chunk object = %v, want chat.completion.chunk", c.Payload["object"])
		}
		delta := sseChunkDelta(c)
		if delta["role"] == "assistant" {
			foundRole = true
		}
		if ct, _ := delta["content"].(string); ct != "" {
			foundContent = true
		}
		if fr := sseChunkFinishReason(c); fr != "" {
			foundFinish = true
		}
	}

	if !foundRole {
		t.Fatal("stream missing role:assistant delta")
	}
	if !foundContent {
		t.Fatal("stream missing content delta")
	}
	if !foundFinish {
		t.Fatal("stream missing finish_reason chunk")
	}
	if !foundDone {
		t.Fatal("stream missing data: [DONE] terminator")
	}
}

// Non-stream tool calling returns protocol-correct tool_calls.
func TestOpenAIHandler_ChatCompletions_NonStream_ToolCalls(t *testing.T) {
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Kiro returns content followed by a tool call with start+stop in one event.
		_, _ = fmt.Fprint(w, `{"content":"Let me check."}{"name":"get_weather","toolUseId":"call_abc","input":"{\"city\":\"NYC\"}","stop":true}`)
	}))
	defer kiro.Close()

	h := NewOpenAIHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	body := `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"weather in nyc?"}],"stream":false,"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	choices, ok := resp["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Fatal("choices is empty or missing")
	}
	choice := choices[0].(map[string]any)

	// finish_reason must be "tool_calls"
	fr, _ := choice["finish_reason"].(string)
	if fr != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", fr)
	}

	message, ok := choice["message"].(map[string]any)
	if !ok {
		t.Fatal("message is missing")
	}

	toolCallsRaw, ok := message["tool_calls"]
	if !ok {
		t.Fatal("message.tool_calls is missing")
	}

	toolCallsJSON, _ := json.Marshal(toolCallsRaw)
	var toolCalls []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(toolCallsJSON, &toolCalls); err != nil {
		t.Fatalf("decode tool_calls: %v (raw=%s)", err, string(toolCallsJSON))
	}

	if len(toolCalls) == 0 {
		t.Fatal("tool_calls array is empty")
	}

	tc := toolCalls[0]
	if tc.ID == "" {
		t.Fatal("tool_calls[0].id is empty")
	}
	if tc.Type != "function" {
		t.Fatalf("tool_calls[0].type = %q, want function", tc.Type)
	}
	if tc.Function.Name != "get_weather" {
		t.Fatalf("tool_calls[0].function.name = %q, want get_weather", tc.Function.Name)
	}
	if !strings.Contains(tc.Function.Arguments, "NYC") {
		t.Fatalf("tool_calls[0].function.arguments = %q, want it to contain NYC", tc.Function.Arguments)
	}
}

// Streaming tool calling returns protocol-correct tool_calls in SSE.
func TestOpenAIHandler_ChatCompletions_Stream_ToolCalls(t *testing.T) {
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"content":"Checking..."}{"name":"get_weather","toolUseId":"call_xyz","input":"{\"city\":\"London\"}","stop":true}`)
	}))
	defer kiro.Close()

	h := NewOpenAIHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	body := `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"weather?"}],"stream":true,"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	chunks := parseSSEChunks(t, rr.Body.String())
	assertStreamToolCallChunk(t, chunks)
}

// OpenAI base64 vision inputs produce image-grounded responses.
func TestOpenAIHandler_ChatCompletions_NonStream_Vision(t *testing.T) {
	var receivedImages bool
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Decode request to verify images are present in the Kiro payload.
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
			convState, _ := payload["conversationState"].(map[string]any)
			cm, _ := convState["currentMessage"].(map[string]any)
			ui, _ := cm["userInputMessage"].(map[string]any)
			if imgs, ok := ui["images"].([]any); ok && len(imgs) > 0 {
				receivedImages = true
				img0, _ := imgs[0].(map[string]any)
				if img0["format"] != "png" {
					t.Errorf("kiro payload image format = %v, want png", img0["format"])
				}
				source, _ := img0["source"].(map[string]any)
				if source["bytes"] != testPNGBase64 {
					t.Errorf("kiro payload image data mismatch")
				}
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"content":"I can see a small red square in the image."}`)
	}))
	defer kiro.Close()

	h := NewOpenAIHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	// OpenAI vision request with base64 data URL image (valid 10×10 PNG).
	body := `{
		"model": "claude-sonnet-4",
		"messages": [{
			"role": "user",
			"content": [
				{"type": "text", "text": "What do you see in this image?"},
				{"type": "image_url", "image_url": {"url": "` + testPNGDataURL + `"}}
			]
		}],
		"stream": false
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !receivedImages {
		t.Fatal("kiro mock did not receive images in the request payload")
	}

	resp := assertOpenAICompletionShape(t, rr.Body.Bytes())
	choice := resp["choices"].([]any)[0].(map[string]any)
	content, _ := choice["message"].(map[string]any)["content"].(string)
	if content == "" {
		t.Fatal("response content is empty")
	}
	if !strings.Contains(content, "image") {
		t.Fatalf("response content = %q, expected image-grounded answer", content)
	}
}

// OpenAI base64 vision inputs work in streaming mode.
func TestOpenAIHandler_ChatCompletions_Stream_Vision(t *testing.T) {
	var receivedImages bool
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
			convState, _ := payload["conversationState"].(map[string]any)
			cm, _ := convState["currentMessage"].(map[string]any)
			ui, _ := cm["userInputMessage"].(map[string]any)
			if imgs, ok := ui["images"].([]any); ok && len(imgs) > 0 {
				receivedImages = true
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"content":"I see a red pixel in this image."}`)
	}))
	defer kiro.Close()

	h := NewOpenAIHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	// Streaming vision request with valid 10×10 PNG.
	body := `{
		"model": "claude-sonnet-4",
		"messages": [{
			"role": "user",
			"content": [
				{"type": "text", "text": "Describe the image."},
				{"type": "image_url", "image_url": {"url": "` + testPNGDataURL + `"}}
			]
		}],
		"stream": true
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !receivedImages {
		t.Fatal("kiro mock did not receive images in the streaming request payload")
	}
	if got := rr.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	resp := rr.Body.String()
	if !strings.Contains(resp, "data: [DONE]") {
		t.Fatal("stream missing [DONE] terminator")
	}
	if !strings.Contains(resp, "image") {
		t.Fatal("stream response should contain image-grounded content")
	}
}

// OpenAI vision with history images preserves images in Kiro history.
func TestOpenAIHandler_ChatCompletions_Vision_History(t *testing.T) {
	var receivedHistoryImages bool
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
			convState, _ := payload["conversationState"].(map[string]any)
			if hist, ok := convState["history"].([]any); ok {
				for _, h := range hist {
					hm, _ := h.(map[string]any)
					if ui, ok := hm["userInputMessage"].(map[string]any); ok {
						if imgs, ok := ui["images"].([]any); ok && len(imgs) > 0 {
							receivedHistoryImages = true
						}
					}
				}
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"content":"That was the red pixel image."}`)
	}))
	defer kiro.Close()

	h := NewOpenAIHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	// History vision request with valid 10×10 PNG in the first user message.
	body := `{
		"model": "claude-sonnet-4",
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "What is this?"},
				{"type": "image_url", "image_url": {"url": "` + testPNGDataURL + `"}}
			]},
			{"role": "assistant", "content": "It's a small image."},
			{"role": "user", "content": "Tell me more about that image."}
		],
		"stream": false
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !receivedHistoryImages {
		t.Fatal("kiro mock did not receive history images in the request payload")
	}
}

// assertStreamToolCallChunk finds and validates the tool_calls delta chunk in SSE output.
func assertStreamToolCallChunk(t *testing.T, chunks []sseChunk) {
	t.Helper()

	var foundDone bool
	for _, c := range chunks {
		if c.IsDone {
			foundDone = true
			continue
		}
		delta := sseChunkDelta(c)
		tcs, ok := delta["tool_calls"].([]any)
		if !ok {
			continue
		}

		// Found the tool_calls delta — verify structure.
		if len(tcs) == 0 {
			t.Fatal("tool_calls delta is empty")
		}
		tc := tcs[0].(map[string]any)
		if tc["id"] == nil || tc["id"] == "" {
			t.Fatal("tool_calls[0].id is empty")
		}
		if tc["type"] != "function" {
			t.Fatalf("tool_calls[0].type = %v, want function", tc["type"])
		}
		fn, _ := tc["function"].(map[string]any)
		if fn["name"] != "get_weather" {
			t.Fatalf("tool_calls[0].function.name = %v, want get_weather", fn["name"])
		}
		return // Found and verified
	}

	if !foundDone {
		t.Fatal("stream missing data: [DONE] terminator")
	}
	t.Fatal("no tool_calls delta found in stream")
}
