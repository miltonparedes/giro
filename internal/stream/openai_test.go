package stream

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- Helpers ---

// sseToSlice drains an SSE string channel into a slice.
func sseToSlice(ch <-chan string) []string {
	var out []string
	for s := range ch {
		out = append(out, s)
	}
	return out
}

// parseOpenAIData extracts the JSON payload from an "data: {json}\n\n" SSE line.
// Returns nil if the line is "data: [DONE]\n\n" or malformed.
func parseOpenAIData(raw string) map[string]any {
	s := strings.TrimPrefix(raw, "data: ")
	s = strings.TrimSpace(s)
	if s == "[DONE]" || s == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	return m
}

// feedEvents creates a KiroEvent channel and feeds the given events into it.
func feedEvents(events ...KiroEvent) <-chan KiroEvent {
	ch := make(chan KiroEvent, len(events))
	for _, evt := range events {
		ch <- evt
	}
	close(ch)
	return ch
}

// --- FormatOpenAISSE tests ---

func TestFormatOpenAISSE_SimpleContent(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "content", Content: "Hello"},
		KiroEvent{Type: "content", Content: " World"},
	)

	cfg := OpenAIStreamConfig{Model: "claude-sonnet-4"}
	chunks := sseToSlice(FormatOpenAISSE(events, cfg))

	// Must have at least: role chunk, 2 content chunks, final chunk, [DONE]
	if len(chunks) < 5 {
		t.Fatalf("expected at least 5 chunks, got %d", len(chunks))
	}

	// First chunk should be role: assistant.
	first := parseOpenAIData(chunks[0])
	if first == nil {
		t.Fatal("first chunk is nil or unparseable")
	}
	choices, _ := first["choices"].([]any)
	if len(choices) == 0 {
		t.Fatal("first chunk has no choices")
	}
	delta, _ := choices[0].(map[string]any)["delta"].(map[string]any)
	if delta["role"] != "assistant" {
		t.Fatalf("expected role assistant, got %v", delta["role"])
	}

	// Content chunks should contain Hello and World.
	var contentParts []string
	for _, chunk := range chunks {
		m := parseOpenAIData(chunk)
		if m == nil {
			continue
		}
		cs, _ := m["choices"].([]any)
		if len(cs) == 0 {
			continue
		}
		d, _ := cs[0].(map[string]any)["delta"].(map[string]any)
		if c, ok := d["content"].(string); ok && c != "" {
			contentParts = append(contentParts, c)
		}
	}
	combined := strings.Join(contentParts, "")
	if combined != "Hello World" {
		t.Fatalf("expected content %q, got %q", "Hello World", combined)
	}

	// Last chunk must be [DONE].
	last := chunks[len(chunks)-1]
	if !strings.Contains(last, "[DONE]") {
		t.Fatalf("expected [DONE] at end, got %q", last)
	}
}

// findOpenAIToolChunk returns the first parsed chunk whose delta contains tool_calls.
func findOpenAIToolChunk(chunks []string) map[string]any {
	for _, c := range chunks {
		m := parseOpenAIData(c)
		if m == nil {
			continue
		}
		cs, _ := m["choices"].([]any)
		if len(cs) == 0 {
			continue
		}
		d, _ := cs[0].(map[string]any)["delta"].(map[string]any)
		if _, ok := d["tool_calls"]; ok {
			return m
		}
	}
	return nil
}

// findOpenAIFinishReason scans chunks from the end and returns the first non-empty
// finish_reason.
func findOpenAIFinishReason(chunks []string) string {
	for i := len(chunks) - 1; i >= 0; i-- {
		m := parseOpenAIData(chunks[i])
		if m == nil {
			continue
		}
		cs, _ := m["choices"].([]any)
		if len(cs) == 0 {
			continue
		}
		fr, ok := cs[0].(map[string]any)["finish_reason"].(string)
		if ok && fr != "" {
			return fr
		}
	}
	return ""
}

// toolCallsChunks returns SSE chunks for a stream with content and a tool call.
func toolCallsChunks() []string {
	events := feedEvents(
		KiroEvent{Type: "content", Content: "Checking..."},
		KiroEvent{
			Type: "tool_use",
			ToolUse: &ToolUseEvent{
				ID:        "call_abc",
				Name:      "get_weather",
				Arguments: `{"city":"NYC"}`,
			},
		},
	)
	cfg := OpenAIStreamConfig{Model: "claude-sonnet-4"}
	return sseToSlice(FormatOpenAISSE(events, cfg))
}

func TestFormatOpenAISSE_WithToolCalls(t *testing.T) {
	chunks := toolCallsChunks()

	toolChunk := findOpenAIToolChunk(chunks)
	if toolChunk == nil {
		t.Fatal("expected a tool_calls chunk")
	}

	// Verify the tool call has an index, correct ID and name.
	cs, _ := toolChunk["choices"].([]any)
	d, _ := cs[0].(map[string]any)["delta"].(map[string]any)
	tcs, _ := d["tool_calls"].([]any)
	if len(tcs) == 0 {
		t.Fatal("expected at least one tool call")
	}
	tc := tcs[0].(map[string]any)
	if _, ok := tc["index"]; !ok {
		t.Fatal("tool call missing index field")
	}
	if tc["id"] != "call_abc" {
		t.Fatalf("expected tool ID %q, got %v", "call_abc", tc["id"])
	}
	fn, _ := tc["function"].(map[string]any)
	if fn["name"] != "get_weather" {
		t.Fatalf("expected tool name %q, got %v", "get_weather", fn["name"])
	}
}

func TestFormatOpenAISSE_ToolCallsFinishReason(t *testing.T) {
	chunks := toolCallsChunks()

	finishReason := findOpenAIFinishReason(chunks)
	if finishReason != "tool_calls" {
		t.Fatalf("expected finish_reason %q, got %q", "tool_calls", finishReason)
	}
}

func TestFormatOpenAISSE_FinishReasonStop(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "content", Content: "Hello"},
	)

	cfg := OpenAIStreamConfig{Model: "claude-sonnet-4"}
	chunks := sseToSlice(FormatOpenAISSE(events, cfg))

	// Find the final chunk with finish_reason.
	var finishReason string
	for i := len(chunks) - 1; i >= 0; i-- {
		m := parseOpenAIData(chunks[i])
		if m == nil {
			continue
		}
		cs, _ := m["choices"].([]any)
		if len(cs) == 0 {
			continue
		}
		fr, ok := cs[0].(map[string]any)["finish_reason"].(string)
		if ok && fr != "" {
			finishReason = fr
			break
		}
	}
	if finishReason != "stop" {
		t.Fatalf("expected finish_reason %q, got %q", "stop", finishReason)
	}
}

func TestFormatOpenAISSE_DoneAtEnd(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "content", Content: "Hello"},
	)

	cfg := OpenAIStreamConfig{Model: "claude-sonnet-4"}
	chunks := sseToSlice(FormatOpenAISSE(events, cfg))

	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}
	last := chunks[len(chunks)-1]
	if last != "data: [DONE]\n\n" {
		t.Fatalf("expected last chunk to be %q, got %q", "data: [DONE]\n\n", last)
	}
}

func TestFormatOpenAISSE_ErrorEvent(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "error", Error: &FirstTokenTimeoutError{}},
	)

	cfg := OpenAIStreamConfig{Model: "claude-sonnet-4"}
	chunks := sseToSlice(FormatOpenAISSE(events, cfg))

	// Should have the error chunk but NOT [DONE] (error returns early).
	var foundError bool
	for _, c := range chunks {
		if strings.Contains(c, "first token timeout") {
			foundError = true
		}
	}
	if !foundError {
		t.Fatal("expected an error chunk with timeout message")
	}

	// Should NOT end with [DONE] since error returns early.
	last := chunks[len(chunks)-1]
	if strings.Contains(last, "[DONE]") {
		t.Fatal("expected no [DONE] after error event")
	}
}

func TestFormatOpenAISSE_ThinkingAsReasoning(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "thinking", ThinkingContent: "Let me think..."},
		KiroEvent{Type: "content", Content: "Answer"},
	)

	cfg := OpenAIStreamConfig{
		Model:            "claude-sonnet-4",
		ThinkingHandling: HandlingAsReasoning,
	}
	chunks := sseToSlice(FormatOpenAISSE(events, cfg))

	var foundReasoning bool
	for _, c := range chunks {
		m := parseOpenAIData(c)
		if m == nil {
			continue
		}
		cs, _ := m["choices"].([]any)
		if len(cs) == 0 {
			continue
		}
		d, _ := cs[0].(map[string]any)["delta"].(map[string]any)
		if _, ok := d["reasoning_content"]; ok {
			foundReasoning = true
			break
		}
	}
	if !foundReasoning {
		t.Fatal("expected reasoning_content delta for thinking event")
	}
}

func TestFormatOpenAISSE_ThinkingRemoved(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "thinking", ThinkingContent: "secret thoughts"},
		KiroEvent{Type: "content", Content: "Answer"},
	)

	cfg := OpenAIStreamConfig{
		Model:            "claude-sonnet-4",
		ThinkingHandling: HandlingRemove,
	}
	chunks := sseToSlice(FormatOpenAISSE(events, cfg))

	for _, c := range chunks {
		if strings.Contains(c, "secret thoughts") {
			t.Fatal("thinking content should be removed, but found it in output")
		}
	}
}

func TestFormatOpenAISSE_UsageInFinalChunk(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "content", Content: "Hello"},
		KiroEvent{Type: "usage", Usage: map[string]any{"credits": 0.001}},
	)

	cfg := OpenAIStreamConfig{Model: "claude-sonnet-4"}
	chunks := sseToSlice(FormatOpenAISSE(events, cfg))

	// The chunk before [DONE] should have credits_used.
	if len(chunks) < 2 {
		t.Fatal("expected at least 2 chunks")
	}
	finalChunk := chunks[len(chunks)-2] // before [DONE]
	if !strings.Contains(finalChunk, "credits_used") {
		t.Fatal("expected credits_used in final chunk before [DONE]")
	}
}

func TestFormatOpenAISSE_ModelName(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "content", Content: "Hello"},
	)

	cfg := OpenAIStreamConfig{Model: "claude-opus-4"}
	chunks := sseToSlice(FormatOpenAISSE(events, cfg))

	var foundModel bool
	for _, c := range chunks {
		m := parseOpenAIData(c)
		if m == nil {
			continue
		}
		if m["model"] == "claude-opus-4" {
			foundModel = true
			break
		}
	}
	if !foundModel {
		t.Fatal("expected model name claude-opus-4 in chunks")
	}
}

// --- CollectOpenAIResponse tests ---

func TestCollectOpenAIResponse_ContentOnly(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "content", Content: "Hello"},
		KiroEvent{Type: "content", Content: " World"},
	)

	cfg := OpenAIStreamConfig{Model: "claude-sonnet-4"}
	resp, err := CollectOpenAIResponse(events, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Object != "chat.completion" {
		t.Fatalf("expected object %q, got %q", "chat.completion", resp.Object)
	}
	if resp.Model != "claude-sonnet-4" {
		t.Fatalf("expected model %q, got %q", "claude-sonnet-4", resp.Model)
	}
	if !strings.HasPrefix(resp.ID, "chatcmpl-") {
		t.Fatalf("expected ID prefix chatcmpl-, got %q", resp.ID)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}

	msg := resp.Choices[0].Message
	content, _ := msg["content"].(string)
	if content != "Hello World" {
		t.Fatalf("expected content %q, got %q", "Hello World", content)
	}

	if *resp.Choices[0].FinishReason != "stop" {
		t.Fatalf("expected finish_reason %q, got %q", "stop", *resp.Choices[0].FinishReason)
	}
}

func TestCollectOpenAIResponse_WithToolCalls(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "content", Content: "Checking..."},
		KiroEvent{
			Type: "tool_use",
			ToolUse: &ToolUseEvent{
				ID:        "call_abc",
				Name:      "get_weather",
				Arguments: `{"city":"NYC"}`,
			},
		},
	)

	cfg := OpenAIStreamConfig{Model: "claude-sonnet-4"}
	resp, err := CollectOpenAIResponse(events, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msg := resp.Choices[0].Message
	tcs, ok := msg["tool_calls"]
	if !ok {
		t.Fatal("expected tool_calls in message")
	}

	toolCalls, _ := json.Marshal(tcs)
	if !strings.Contains(string(toolCalls), "get_weather") {
		t.Fatalf("expected get_weather in tool_calls, got %s", string(toolCalls))
	}

	if *resp.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("expected finish_reason %q, got %q", "tool_calls", *resp.Choices[0].FinishReason)
	}
}

func TestCollectOpenAIResponse_WithThinkingAsReasoning(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "thinking", ThinkingContent: "Let me think..."},
		KiroEvent{Type: "content", Content: "Answer"},
	)

	cfg := OpenAIStreamConfig{
		Model:            "claude-sonnet-4",
		ThinkingHandling: HandlingAsReasoning,
	}
	resp, err := CollectOpenAIResponse(events, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msg := resp.Choices[0].Message
	rc, ok := msg["reasoning_content"].(string)
	if !ok {
		t.Fatal("expected reasoning_content in message")
	}
	if rc != "Let me think..." {
		t.Fatalf("expected reasoning_content %q, got %q", "Let me think...", rc)
	}

	content, _ := msg["content"].(string)
	if content != "Answer" {
		t.Fatalf("expected content %q, got %q", "Answer", content)
	}
}

func TestCollectOpenAIResponse_Error(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "error", Error: &FirstTokenTimeoutError{}},
	)

	cfg := OpenAIStreamConfig{Model: "claude-sonnet-4"}
	_, err := CollectOpenAIResponse(events, cfg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if err.Error() != "first token timeout" {
		t.Fatalf("expected error %q, got %q", "first token timeout", err.Error())
	}
}

func TestCollectOpenAIResponse_UsagePresent(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "content", Content: "Hello"},
	)

	cfg := OpenAIStreamConfig{Model: "claude-sonnet-4"}
	resp, err := CollectOpenAIResponse(events, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Usage struct should be present (zero values).
	if resp.Usage.PromptTokens != 0 || resp.Usage.CompletionTokens != 0 {
		t.Fatalf("expected zero usage, got %+v", resp.Usage)
	}
}
