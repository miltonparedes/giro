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

// --- VAL-ANTHROPIC assertion-targeted tests --------------------------------

// VAL-ANTHROPIC-001: Non-stream /v1/messages succeeds with correct response shape.
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

// VAL-ANTHROPIC-003: Malformed JSON yields an Anthropic-style error envelope.
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

// VAL-ANTHROPIC-004: Streaming /v1/messages returns the full Anthropic SSE lifecycle.
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

// VAL-ANTHROPIC-005: Anthropic tool use returns a tool_use block with stop_reason tool_use.
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

// VAL-ANTHROPIC-006: Tool-result continuation is accepted on the next turn.
func TestAnthropicHandler_Messages_ToolResultContinuation(t *testing.T) {
	kiro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Always return text content; the test verifies the handler accepts the
		// tool-result payload and produces a final assistant text answer.
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

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp["stop_reason"] != "end_turn" {
		t.Fatalf("stop_reason = %v, want end_turn", resp["stop_reason"])
	}

	content, ok := resp["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatal("content is missing or empty")
	}

	// The final response should contain a text block.
	block := content[0].(map[string]any)
	if block["type"] != "text" {
		t.Fatalf("content[0].type = %v, want text", block["type"])
	}
	text, _ := block["text"].(string)
	if text == "" {
		t.Fatal("final text answer is empty")
	}
}

// VAL-ANTHROPIC-008: Streamed Anthropic tool use preserves the tool-use SSE lifecycle.
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
