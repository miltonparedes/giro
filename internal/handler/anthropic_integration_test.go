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

func TestAnthropicHandler_Messages_InvalidBody(t *testing.T) {
	h := NewAnthropicHandler(
		newTestAuthManager(t, "http://127.0.0.1:1", "http://127.0.0.1:1"),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("{"))
	rr := httptest.NewRecorder()
	h.Messages(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rr.Body.String(), "invalid request body") {
		t.Fatalf("unexpected error body: %q", rr.Body.String())
	}
}

func TestAnthropicHandler_Messages_NonStream_Success(t *testing.T) {
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"content":"hello from kiro"}`)
	}))
	defer kiro.Close()

	h := NewAnthropicHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	body := `{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.Messages(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"type":"message"`) {
		t.Fatalf("unexpected anthropic response body: %q", rr.Body.String())
	}
}

func TestAnthropicHandler_Messages_Stream_Success(t *testing.T) {
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"content":"hello stream"}`)
	}))
	defer kiro.Close()

	h := NewAnthropicHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	body := `{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.Messages(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := rr.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	resp := rr.Body.String()
	if !strings.Contains(resp, "event: message_start") {
		t.Fatalf("stream output missing message_start: %q", resp)
	}
	if !strings.Contains(resp, "event: message_stop") {
		t.Fatalf("stream output missing message_stop: %q", resp)
	}
}

func TestAnthropicHandler_Messages_UpstreamJSONError(t *testing.T) {
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"message":"ctx too long","reason":"CONTENT_LENGTH_EXCEEDS_THRESHOLD"}`)
	}))
	defer kiro.Close()

	h := NewAnthropicHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	body := `{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.Messages(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rr.Body.String(), "Model context limit reached") {
		t.Fatalf("unexpected error body: %q", rr.Body.String())
	}
}

func TestAnthropicHandler_Messages_UpstreamTextError(t *testing.T) {
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = fmt.Fprint(w, "boom")
	}))
	defer kiro.Close()

	h := NewAnthropicHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	body := `{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.Messages(rr, req)

	if rr.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusTeapot)
	}
	if !strings.Contains(rr.Body.String(), "boom") {
		t.Fatalf("unexpected error body: %q", rr.Body.String())
	}
}

func TestAnthropicHandler_Messages_FirstTokenRetry_Succeeds(t *testing.T) {
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

	h := NewAnthropicHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		cfg,
	)

	body := `{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.Messages(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestAnthropicHandler_Messages_FirstTokenRetry_Exhausted(t *testing.T) {
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer kiro.Close()

	cfg := testHandlerConfig()
	cfg.FirstTokenMaxRetries = 2

	h := NewAnthropicHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		cfg,
	)

	body := `{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.Messages(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadGateway)
	}
	if !strings.Contains(rr.Body.String(), "all 2 first-token retry attempts failed") {
		t.Fatalf("unexpected error body: %q", rr.Body.String())
	}
}

// --- Anthropic assertion-targeted tests ------------------------------------

// Non-stream /v1/messages succeeds with correct response shape.
func TestAnthropicHandler_Messages_NonStream_FullResponseShape(t *testing.T) {
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"content":"hello from kiro"}`)
	}))
	defer kiro.Close()

	h := NewAnthropicHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	body := `{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.Messages(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	resp := assertAnthropicMessageShape(t, rr.Body.Bytes())

	// Must have at least one content block.
	content, _ := resp["content"].([]any)
	if len(content) == 0 {
		t.Fatal("content array is empty")
	}
	block := content[0].(map[string]any)
	if block["type"] != "text" {
		t.Fatalf("content[0].type = %v, want text", block["type"])
	}
	text, _ := block["text"].(string)
	if text == "" {
		t.Fatal("content[0].text is empty")
	}

	sr, _ := resp["stop_reason"].(string)
	if sr != "end_turn" {
		t.Fatalf("stop_reason = %q, want end_turn", sr)
	}
}

// Malformed JSON yields an Anthropic-style error envelope.
func TestAnthropicHandler_Messages_MalformedJSON_ErrorEnvelope(t *testing.T) {
	h := NewAnthropicHandler(
		newTestAuthManager(t, "http://127.0.0.1:1", "http://127.0.0.1:1"),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("{invalid"))
	rr := httptest.NewRecorder()
	h.Messages(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	if body["type"] != "error" {
		t.Fatalf("top-level type = %v, want error", body["type"])
	}

	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("response missing 'error' object")
	}
	if errObj["type"] == nil || errObj["type"] == "" {
		t.Fatal("error.type is missing")
	}
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "invalid request body") {
		t.Fatalf("error.message = %q, want it to mention invalid request body", msg)
	}
}

// Streaming /v1/messages returns the full Anthropic SSE lifecycle.
func TestAnthropicHandler_Messages_Stream_FullLifecycle(t *testing.T) {
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"content":"streamed answer"}`)
	}))
	defer kiro.Close()

	h := NewAnthropicHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	body := `{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.Messages(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := rr.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}

	events := parseAnthropicSSEEvents(t, rr.Body.String())
	assertAnthropicSSELifecycle(t, events)
}

// Anthropic tool use returns a tool_use block with stop_reason tool_use.
func TestAnthropicHandler_Messages_NonStream_ToolUse(t *testing.T) {
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"content":"Let me check."}{"name":"get_weather","toolUseId":"call_abc","input":"{\"city\":\"NYC\"}","stop":true}`)
	}))
	defer kiro.Close()

	h := NewAnthropicHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	body := `{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"weather?"}],"stream":false,"tools":[{"name":"get_weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}}}}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.Messages(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp["stop_reason"] != "tool_use" {
		t.Fatalf("stop_reason = %v, want tool_use", resp["stop_reason"])
	}

	content, ok := resp["content"].([]any)
	if !ok {
		t.Fatal("content is missing")
	}

	// Find the tool_use block.
	var foundToolUse bool
	for _, block := range content {
		b, _ := block.(map[string]any)
		if b["type"] != "tool_use" {
			continue
		}
		foundToolUse = true

		if b["id"] == nil || b["id"] == "" {
			t.Fatal("tool_use block id is empty")
		}
		if b["name"] != "get_weather" {
			t.Fatalf("tool_use name = %v, want get_weather", b["name"])
		}
		input, _ := b["input"].(map[string]any)
		if input["city"] != "NYC" {
			t.Fatalf("tool_use input.city = %v, want NYC", input["city"])
		}
	}
	if !foundToolUse {
		t.Fatal("no tool_use content block found")
	}
}

// Tool-result continuation forwards tool_use, toolUseId,
// and tool_result intact to Kiro, not just returning a successful surface response.
func TestAnthropicHandler_Messages_ToolResultContinuation(t *testing.T) {
	var capturedPayload map[string]any
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the outbound Kiro request body to verify continuation payload.
		if err := json.NewDecoder(r.Body).Decode(&capturedPayload); err != nil {
			t.Errorf("failed to decode kiro request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"content":"The weather is sunny and 22°C in NYC."}`)
	}))
	defer kiro.Close()

	h := NewAnthropicHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	// Build a continuation request: assistant had a tool_use, user provides tool_result.
	body := `{
		"model": "claude-sonnet-4",
		"max_tokens": 64,
		"messages": [
			{"role": "user", "content": "What is the weather in NYC?"},
			{"role": "assistant", "content": [
				{"type": "text", "text": "Let me check."},
				{"type": "tool_use", "id": "toolu_abc", "name": "get_weather", "input": {"city": "NYC"}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "toolu_abc", "content": "22°C, sunny"}
			]}
		],
		"stream": false,
		"tools": [{"name": "get_weather", "input_schema": {"type": "object", "properties": {"city": {"type": "string"}}}}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.Messages(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	// --- Surface response assertions ---
	assertToolResultContinuationSurface(t, rr.Body.Bytes())

	// --- Outbound Kiro payload assertions (strengthened for tool-result continuation) ---
	if capturedPayload == nil {
		t.Fatal("kiro mock did not receive any request payload")
	}
	assertKiroToolUseContinuationPayload(t, capturedPayload)
}

// assertToolResultContinuationSurface verifies the surface response for a
// tool-result continuation: stop_reason end_turn and a non-empty text block.
func assertToolResultContinuationSurface(t *testing.T, body []byte) {
	t.Helper()

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp["stop_reason"] != "end_turn" {
		t.Fatalf("stop_reason = %v, want end_turn", resp["stop_reason"])
	}

	content, ok := resp["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatal("content is missing or empty")
	}

	block := content[0].(map[string]any)
	if block["type"] != "text" {
		t.Fatalf("content[0].type = %v, want text", block["type"])
	}
	text, _ := block["text"].(string)
	if text == "" {
		t.Fatal("final text answer is empty")
	}
}

// assertKiroToolUseContinuationPayload verifies the outbound Kiro request
// contains the assistant tool_use with matching toolUseId in history and the
// user tool_result with matching toolUseId in the current message context.
func assertKiroToolUseContinuationPayload(t *testing.T, payload map[string]any) {
	t.Helper()

	convState, _ := payload["conversationState"].(map[string]any)
	if convState == nil {
		t.Fatal("kiro payload missing conversationState")
	}

	assertKiroHistoryContainsToolUse(t, convState)
	assertKiroCurrentMessageContainsToolResult(t, convState)
}

// assertKiroHistoryContainsToolUse verifies the history contains an assistant
// turn with a toolUse entry matching toolUseId=toolu_abc, name=get_weather,
// and input.city=NYC.
func assertKiroHistoryContainsToolUse(t *testing.T, convState map[string]any) {
	t.Helper()

	historyRaw, _ := convState["history"].([]any)
	if len(historyRaw) == 0 {
		t.Fatal("kiro payload history is empty; expected assistant tool_use turn")
	}

	for _, entry := range historyRaw {
		hm, _ := entry.(map[string]any)
		arm, _ := hm["assistantResponseMessage"].(map[string]any)
		if arm == nil {
			continue
		}
		toolUses, _ := arm["toolUses"].([]any)
		for _, tu := range toolUses {
			tuMap, _ := tu.(map[string]any)
			if tuMap["toolUseId"] != "toolu_abc" || tuMap["name"] != "get_weather" {
				continue
			}
			input, _ := tuMap["input"].(map[string]any)
			if input["city"] != "NYC" {
				t.Fatalf("kiro history toolUse input.city = %v, want NYC", input["city"])
			}
			return // found and verified
		}
	}
	t.Fatal("kiro history missing assistant toolUse with toolUseId=toolu_abc and name=get_weather")
}

// assertKiroCurrentMessageContainsToolResult verifies the currentMessage
// contains a toolResult with toolUseId=toolu_abc and content matching the
// supplied tool result text.
func assertKiroCurrentMessageContainsToolResult(t *testing.T, convState map[string]any) {
	t.Helper()

	cm, _ := convState["currentMessage"].(map[string]any)
	if cm == nil {
		t.Fatal("kiro payload missing currentMessage")
	}
	ui, _ := cm["userInputMessage"].(map[string]any)
	if ui == nil {
		t.Fatal("kiro payload missing userInputMessage in currentMessage")
	}
	uiCtx, _ := ui["userInputMessageContext"].(map[string]any)
	if uiCtx == nil {
		t.Fatal("kiro payload missing userInputMessageContext; tool_result not forwarded")
	}
	toolResultsRaw, _ := uiCtx["toolResults"].([]any)
	if len(toolResultsRaw) == 0 {
		t.Fatal("kiro payload toolResults is empty; tool_result block was not forwarded")
	}

	for _, tr := range toolResultsRaw {
		trMap, _ := tr.(map[string]any)
		if trMap["toolUseId"] != "toolu_abc" {
			continue
		}
		trContent, _ := trMap["content"].([]any)
		if len(trContent) == 0 {
			t.Fatal("kiro payload toolResult content is empty")
		}
		first, _ := trContent[0].(map[string]any)
		trText, _ := first["text"].(string)
		if trText != "22°C, sunny" {
			t.Fatalf("kiro payload toolResult text = %q, want %q", trText, "22°C, sunny")
		}
		return // found and verified
	}
	t.Fatal("kiro payload missing toolResult with toolUseId=toolu_abc")
}

// Streamed Anthropic tool use preserves the tool-use SSE lifecycle.
func TestAnthropicHandler_Messages_Stream_ToolUse(t *testing.T) {
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"content":"Checking..."}{"name":"get_weather","toolUseId":"call_xyz","input":"{\"city\":\"London\"}","stop":true}`)
	}))
	defer kiro.Close()

	h := NewAnthropicHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	body := `{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"weather?"}],"stream":true,"tools":[{"name":"get_weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}}}}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.Messages(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	events := parseAnthropicSSEEvents(t, rr.Body.String())
	assertAnthropicStreamToolUseLifecycle(t, events, "get_weather", "London")
}

// --- Anthropic assertion helpers ---

// anthropicSSEEvent is a parsed Anthropic SSE event.
type anthropicSSEEvent struct {
	eventType string
	data      map[string]any
}

// parseAnthropicSSEEvents splits raw SSE output into structured events.
func parseAnthropicSSEEvents(t *testing.T, output string) []anthropicSSEEvent {
	t.Helper()
	var events []anthropicSSEEvent
	for _, block := range strings.Split(output, "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		var evtType string
		var data map[string]any
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(line, "event: ") {
				evtType = strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "data: ") {
				jsonStr := strings.TrimPrefix(line, "data: ")
				_ = json.Unmarshal([]byte(jsonStr), &data)
			}
		}
		if evtType != "" {
			events = append(events, anthropicSSEEvent{eventType: evtType, data: data})
		}
	}
	return events
}

// assertAnthropicMessageShape verifies the full shape of a non-streaming
// Anthropic message response and returns the parsed body.
func assertAnthropicMessageShape(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["type"] != "message" {
		t.Fatalf("type = %v, want message", resp["type"])
	}

	id, _ := resp["id"].(string)
	if !strings.HasPrefix(id, "msg_") {
		t.Fatalf("id = %q, want msg_ prefix", id)
	}

	if resp["role"] != "assistant" {
		t.Fatalf("role = %v, want assistant", resp["role"])
	}

	if resp["model"] == nil || resp["model"] == "" {
		t.Fatal("model is missing")
	}

	content, ok := resp["content"].([]any)
	if !ok {
		t.Fatal("content is missing or not an array")
	}
	_ = content // caller checks content further

	return resp
}

// assertAnthropicSSELifecycle verifies the full Anthropic SSE lifecycle:
// message_start, at least one content block event, message_delta, message_stop.
func assertAnthropicSSELifecycle(t *testing.T, events []anthropicSSEEvent) {
	t.Helper()

	if len(events) == 0 {
		t.Fatal("no SSE events found")
	}

	// First event must be message_start.
	if events[0].eventType != "message_start" {
		t.Fatalf("first event = %q, want message_start", events[0].eventType)
	}

	// Verify message_start shape.
	msg, _ := events[0].data["message"].(map[string]any)
	if msg["role"] != "assistant" {
		t.Fatalf("message_start role = %v, want assistant", msg["role"])
	}
	msgID, _ := msg["id"].(string)
	if !strings.HasPrefix(msgID, "msg_") {
		t.Fatalf("message_start id = %q, want msg_ prefix", msgID)
	}

	// Last event must be message_stop.
	last := events[len(events)-1]
	if last.eventType != "message_stop" {
		t.Fatalf("last event = %q, want message_stop", last.eventType)
	}

	// Must have at least one content_block event.
	var foundContentBlock, foundMessageDelta bool
	for _, evt := range events {
		switch evt.eventType {
		case "content_block_start", "content_block_delta", "content_block_stop":
			foundContentBlock = true
		case "message_delta":
			foundMessageDelta = true
			delta, _ := evt.data["delta"].(map[string]any)
			if delta["stop_reason"] == nil {
				t.Fatal("message_delta missing stop_reason")
			}
		}
	}

	if !foundContentBlock {
		t.Fatal("no content_block events found in SSE stream")
	}
	if !foundMessageDelta {
		t.Fatal("no message_delta event found in SSE stream")
	}
}

// assertAnthropicStreamToolUseLifecycle verifies that an Anthropic SSE stream
// contains the tool-use lifecycle: content_block_start for tool_use,
// input_json_delta, message_delta with stop_reason:"tool_use", and message_stop.
func assertAnthropicStreamToolUseLifecycle(t *testing.T, events []anthropicSSEEvent, toolName, inputSubstring string) {
	t.Helper()

	var foundToolBlockStart, foundInputDelta, foundToolStopReason, foundMessageStop bool

	for _, evt := range events {
		assertToolUseSSEEvent(t, evt, toolName, inputSubstring,
			&foundToolBlockStart, &foundInputDelta, &foundToolStopReason, &foundMessageStop)
	}

	if !foundToolBlockStart {
		t.Fatal("stream missing content_block_start for tool_use")
	}
	if !foundInputDelta {
		t.Fatal("stream missing input_json_delta event")
	}
	if !foundToolStopReason {
		t.Fatal("stream missing message_delta with stop_reason:tool_use")
	}
	if !foundMessageStop {
		t.Fatal("stream missing message_stop")
	}
}

// Anthropic base64 vision inputs produce image-grounded responses.
func TestAnthropicHandler_Messages_NonStream_Vision(t *testing.T) {
	var receivedImages bool
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	h := NewAnthropicHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	// Anthropic vision request with valid 10×10 PNG base64 image block.
	body := `{
		"model": "claude-sonnet-4",
		"max_tokens": 64,
		"messages": [{
			"role": "user",
			"content": [
				{"type": "text", "text": "What do you see in this image?"},
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
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.Messages(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !receivedImages {
		t.Fatal("kiro mock did not receive images in the request payload")
	}

	resp := assertAnthropicMessageShape(t, rr.Body.Bytes())
	content, _ := resp["content"].([]any)
	if len(content) == 0 {
		t.Fatal("content array is empty")
	}
	block := content[0].(map[string]any)
	text, _ := block["text"].(string)
	if text == "" {
		t.Fatal("response text is empty")
	}
	if !strings.Contains(text, "image") {
		t.Fatalf("response text = %q, expected image-grounded answer", text)
	}
}

// Anthropic base64 vision inputs work in streaming mode.
func TestAnthropicHandler_Messages_Stream_Vision(t *testing.T) {
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
		_, _ = fmt.Fprint(w, `{"content":"The image shows a small red pixel."}`)
	}))
	defer kiro.Close()

	h := NewAnthropicHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	// Streaming vision request with valid 10×10 PNG.
	body := `{
		"model": "claude-sonnet-4",
		"max_tokens": 64,
		"messages": [{
			"role": "user",
			"content": [
				{"type": "text", "text": "Describe this image."},
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
		"stream": true
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.Messages(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !receivedImages {
		t.Fatal("kiro mock did not receive images in the streaming request payload")
	}
	if got := rr.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}

	events := parseAnthropicSSEEvents(t, rr.Body.String())
	assertAnthropicSSELifecycle(t, events)
}

// Anthropic vision with history images preserves images in Kiro history.
func TestAnthropicHandler_Messages_Vision_History(t *testing.T) {
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
		_, _ = fmt.Fprint(w, `{"content":"That was the image from before."}`)
	}))
	defer kiro.Close()

	h := NewAnthropicHandler(
		newTestAuthManager(t, kiro.URL, kiro.URL),
		newTestResolver("claude-sonnet-4"),
		newTestHTTPClient(),
		testHandlerConfig(),
	)

	// History vision request with valid 10×10 PNG in the first user message.
	body := `{
		"model": "claude-sonnet-4",
		"max_tokens": 64,
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "What is this?"},
				{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "` + testPNGBase64 + `"}}
			]},
			{"role": "assistant", "content": [{"type": "text", "text": "It's a small image."}]},
			{"role": "user", "content": "Tell me more about that image."}
		],
		"stream": false
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.Messages(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !receivedHistoryImages {
		t.Fatal("kiro mock did not receive history images in the request payload")
	}
}

// assertToolUseSSEEvent checks a single SSE event against tool-use expectations.
func assertToolUseSSEEvent(
	t *testing.T, evt anthropicSSEEvent, toolName, inputSubstring string,
	foundToolBlockStart, foundInputDelta, foundToolStopReason, foundMessageStop *bool,
) {
	t.Helper()

	switch evt.eventType {
	case "content_block_start":
		block, _ := evt.data["content_block"].(map[string]any)
		if block["type"] == "tool_use" {
			*foundToolBlockStart = true
			if block["name"] != toolName {
				t.Fatalf("tool_use name = %v, want %s", block["name"], toolName)
			}
			if block["id"] == nil || block["id"] == "" {
				t.Fatal("tool_use block id is empty")
			}
		}
	case "content_block_delta":
		delta, _ := evt.data["delta"].(map[string]any)
		if delta["type"] == "input_json_delta" {
			*foundInputDelta = true
			pj, _ := delta["partial_json"].(string)
			if !strings.Contains(pj, inputSubstring) {
				t.Fatalf("input_json_delta partial_json = %q, want it to contain %s", pj, inputSubstring)
			}
		}
	case "message_delta":
		delta, _ := evt.data["delta"].(map[string]any)
		if delta["stop_reason"] == "tool_use" {
			*foundToolStopReason = true
		}
	case "message_stop":
		*foundMessageStop = true
	}
}
