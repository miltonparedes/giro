package stream

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- Helpers ---

// parseAnthropicEvent extracts the event type and data from an Anthropic SSE
// string like "event: {type}\ndata: {json}\n\n".
func parseAnthropicEvent(raw string) (string, map[string]any) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	var eventType string
	var data map[string]any

	for _, line := range lines {
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		}
		if strings.HasPrefix(line, "data: ") {
			jsonStr := strings.TrimPrefix(line, "data: ")
			_ = json.Unmarshal([]byte(jsonStr), &data)
		}
	}
	return eventType, data
}

// --- FormatAnthropicSSE tests ---

func TestFormatAnthropicSSE_MessageStart(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "content", Content: "Hello"},
	)

	cfg := AnthropicStreamConfig{Model: "claude-sonnet-4"}
	chunks := sseToSlice(FormatAnthropicSSE(events, cfg))

	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	// First chunk should be message_start.
	evtType, data := parseAnthropicEvent(chunks[0])
	if evtType != "message_start" {
		t.Fatalf("expected event type %q, got %q", "message_start", evtType)
	}

	msg, _ := data["message"].(map[string]any)
	if msg["role"] != "assistant" {
		t.Fatalf("expected role assistant, got %v", msg["role"])
	}
	if msg["model"] != "claude-sonnet-4" {
		t.Fatalf("expected model %q, got %v", "claude-sonnet-4", msg["model"])
	}
	id, _ := msg["id"].(string)
	if !strings.HasPrefix(id, "msg_") {
		t.Fatalf("expected message ID prefix msg_, got %q", id)
	}
}

func TestFormatAnthropicSSE_ContentBlockLifecycle(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "content", Content: "Hello"},
		KiroEvent{Type: "content", Content: " World"},
	)

	cfg := AnthropicStreamConfig{Model: "claude-sonnet-4"}
	chunks := sseToSlice(FormatAnthropicSSE(events, cfg))

	var blockStarts, blockDeltas, blockStops int
	for _, c := range chunks {
		evtType, _ := parseAnthropicEvent(c)
		switch evtType {
		case "content_block_start":
			blockStarts++
		case "content_block_delta":
			blockDeltas++
		case "content_block_stop":
			blockStops++
		}
	}

	if blockStarts < 1 {
		t.Fatal("expected at least 1 content_block_start")
	}
	if blockDeltas < 2 {
		t.Fatalf("expected at least 2 content_block_delta, got %d", blockDeltas)
	}
	if blockStops < 1 {
		t.Fatal("expected at least 1 content_block_stop")
	}
}

func TestFormatAnthropicSSE_ContentDelta(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "content", Content: "Hello"},
		KiroEvent{Type: "content", Content: " World"},
	)

	cfg := AnthropicStreamConfig{Model: "claude-sonnet-4"}
	chunks := sseToSlice(FormatAnthropicSSE(events, cfg))

	var textParts []string
	for _, c := range chunks {
		evtType, data := parseAnthropicEvent(c)
		if evtType != "content_block_delta" {
			continue
		}
		delta, _ := data["delta"].(map[string]any)
		if delta["type"] == "text_delta" {
			text, _ := delta["text"].(string)
			textParts = append(textParts, text)
		}
	}

	combined := strings.Join(textParts, "")
	if combined != "Hello World" {
		t.Fatalf("expected combined text %q, got %q", "Hello World", combined)
	}
}

func TestFormatAnthropicSSE_MessageDelta(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "content", Content: "Hello"},
	)

	cfg := AnthropicStreamConfig{Model: "claude-sonnet-4"}
	chunks := sseToSlice(FormatAnthropicSSE(events, cfg))

	var foundDelta bool
	for _, c := range chunks {
		evtType, data := parseAnthropicEvent(c)
		if evtType == "message_delta" {
			foundDelta = true
			delta, _ := data["delta"].(map[string]any)
			if delta["stop_reason"] != "end_turn" {
				t.Fatalf("expected stop_reason %q, got %v", "end_turn", delta["stop_reason"])
			}
		}
	}
	if !foundDelta {
		t.Fatal("expected message_delta event")
	}
}

func TestFormatAnthropicSSE_MessageStop(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "content", Content: "Hello"},
	)

	cfg := AnthropicStreamConfig{Model: "claude-sonnet-4"}
	chunks := sseToSlice(FormatAnthropicSSE(events, cfg))

	last := chunks[len(chunks)-1]
	evtType, _ := parseAnthropicEvent(last)
	if evtType != "message_stop" {
		t.Fatalf("expected last event to be message_stop, got %q", evtType)
	}
}

func TestFormatAnthropicSSE_ToolUseBlocks(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "content", Content: "Let me check"},
		KiroEvent{
			Type: "tool_use",
			ToolUse: &ToolUseEvent{
				ID:        "toolu_abc",
				Name:      "get_weather",
				Arguments: `{"city":"Moscow"}`,
			},
		},
	)

	cfg := AnthropicStreamConfig{Model: "claude-sonnet-4"}
	chunks := sseToSlice(FormatAnthropicSSE(events, cfg))

	// Find tool_use content_block_start.
	var foundToolStart bool
	for _, c := range chunks {
		evtType, data := parseAnthropicEvent(c)
		if evtType != "content_block_start" {
			continue
		}
		block, _ := data["content_block"].(map[string]any)
		if block["type"] == "tool_use" {
			foundToolStart = true
			if block["name"] != "get_weather" {
				t.Fatalf("expected tool name %q, got %v", "get_weather", block["name"])
			}
			if block["id"] != "toolu_abc" {
				t.Fatalf("expected tool ID %q, got %v", "toolu_abc", block["id"])
			}
		}
	}
	if !foundToolStart {
		t.Fatal("expected tool_use content_block_start")
	}

	// Find input_json_delta.
	var foundInputDelta bool
	for _, c := range chunks {
		evtType, data := parseAnthropicEvent(c)
		if evtType != "content_block_delta" {
			continue
		}
		delta, _ := data["delta"].(map[string]any)
		if delta["type"] == "input_json_delta" {
			foundInputDelta = true
			pj, _ := delta["partial_json"].(string)
			if !strings.Contains(pj, "Moscow") {
				t.Fatalf("expected Moscow in partial_json, got %q", pj)
			}
		}
	}
	if !foundInputDelta {
		t.Fatal("expected input_json_delta")
	}
}

func TestFormatAnthropicSSE_StopReasonToolUse(t *testing.T) {
	events := feedEvents(
		KiroEvent{
			Type: "tool_use",
			ToolUse: &ToolUseEvent{
				ID:        "toolu_abc",
				Name:      "func1",
				Arguments: `{}`,
			},
		},
	)

	cfg := AnthropicStreamConfig{Model: "claude-sonnet-4"}
	chunks := sseToSlice(FormatAnthropicSSE(events, cfg))

	for _, c := range chunks {
		evtType, data := parseAnthropicEvent(c)
		if evtType == "message_delta" {
			delta, _ := data["delta"].(map[string]any)
			if delta["stop_reason"] != "tool_use" {
				t.Fatalf("expected stop_reason %q, got %v", "tool_use", delta["stop_reason"])
			}
			return
		}
	}
	t.Fatal("expected message_delta with stop_reason tool_use")
}

func TestFormatAnthropicSSE_ErrorEvent(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "error", Error: &FirstTokenTimeoutError{}},
	)

	cfg := AnthropicStreamConfig{Model: "claude-sonnet-4"}
	chunks := sseToSlice(FormatAnthropicSSE(events, cfg))

	var foundError bool
	for _, c := range chunks {
		evtType, data := parseAnthropicEvent(c)
		if evtType == "error" {
			foundError = true
			errObj, _ := data["error"].(map[string]any)
			if errObj["type"] != "api_error" {
				t.Fatalf("expected error type api_error, got %v", errObj["type"])
			}
			if msg, _ := errObj["message"].(string); msg != "first token timeout" {
				t.Fatalf("expected error message %q, got %q", "first token timeout", msg)
			}
		}
	}
	if !foundError {
		t.Fatal("expected error event")
	}

	// Should NOT have message_stop (error returns early).
	for _, c := range chunks {
		evtType, _ := parseAnthropicEvent(c)
		if evtType == "message_stop" {
			t.Fatal("expected no message_stop after error event")
		}
	}
}

func TestFormatAnthropicSSE_ThinkingAsReasoning(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "thinking", ThinkingContent: "deep thought"},
		KiroEvent{Type: "content", Content: "response"},
	)

	cfg := AnthropicStreamConfig{
		Model:            "claude-sonnet-4",
		ThinkingHandling: HandlingAsReasoning,
	}
	chunks := sseToSlice(FormatAnthropicSSE(events, cfg))

	// Should have a thinking content_block_start.
	var foundThinkingBlock bool
	for _, c := range chunks {
		evtType, data := parseAnthropicEvent(c)
		if evtType != "content_block_start" {
			continue
		}
		block, _ := data["content_block"].(map[string]any)
		if block["type"] == "thinking" {
			foundThinkingBlock = true
			sig, _ := block["signature"].(string)
			if !strings.HasPrefix(sig, "sig_") {
				t.Fatalf("expected signature prefix sig_, got %q", sig)
			}
		}
	}
	if !foundThinkingBlock {
		t.Fatal("expected thinking content_block_start")
	}

	// Should have a thinking_delta.
	var foundThinkingDelta bool
	for _, c := range chunks {
		evtType, data := parseAnthropicEvent(c)
		if evtType != "content_block_delta" {
			continue
		}
		delta, _ := data["delta"].(map[string]any)
		if delta["type"] == "thinking_delta" {
			foundThinkingDelta = true
			thinking, _ := delta["thinking"].(string)
			if thinking != "deep thought" {
				t.Fatalf("expected thinking %q, got %q", "deep thought", thinking)
			}
		}
	}
	if !foundThinkingDelta {
		t.Fatal("expected thinking_delta")
	}
}

func TestFormatAnthropicSSE_ThinkingRemoved(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "thinking", ThinkingContent: "secret thoughts"},
		KiroEvent{Type: "content", Content: "visible response"},
	)

	cfg := AnthropicStreamConfig{
		Model:            "claude-sonnet-4",
		ThinkingHandling: HandlingRemove,
	}
	chunks := sseToSlice(FormatAnthropicSSE(events, cfg))

	for _, c := range chunks {
		if strings.Contains(c, "secret thoughts") {
			t.Fatal("thinking content should be removed, but found it in output")
		}
	}
}

func TestFormatAnthropicSSE_ThinkingAsText(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "thinking", ThinkingContent: "thinking text"},
		KiroEvent{Type: "content", Content: "regular text"},
	)

	cfg := AnthropicStreamConfig{
		Model:            "claude-sonnet-4",
		ThinkingHandling: HandlingPass,
	}
	chunks := sseToSlice(FormatAnthropicSSE(events, cfg))

	// Thinking should be emitted as regular text_delta.
	var foundThinkingAsText bool
	for _, c := range chunks {
		evtType, data := parseAnthropicEvent(c)
		if evtType != "content_block_delta" {
			continue
		}
		delta, _ := data["delta"].(map[string]any)
		if delta["type"] == "text_delta" {
			text, _ := delta["text"].(string)
			if strings.Contains(text, "thinking text") {
				foundThinkingAsText = true
			}
		}
	}
	if !foundThinkingAsText {
		t.Fatal("expected thinking content emitted as text_delta in pass mode")
	}
}

// --- CollectAnthropicResponse tests ---

func TestCollectAnthropicResponse_ContentOnly(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "content", Content: "Hello"},
		KiroEvent{Type: "content", Content: " World"},
	)

	cfg := AnthropicStreamConfig{Model: "claude-sonnet-4"}
	resp, err := CollectAnthropicResponse(events, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Type != "message" {
		t.Fatalf("expected type %q, got %q", "message", resp.Type)
	}
	if resp.Role != "assistant" {
		t.Fatalf("expected role %q, got %q", "assistant", resp.Role)
	}
	if resp.Model != "claude-sonnet-4" {
		t.Fatalf("expected model %q, got %q", "claude-sonnet-4", resp.Model)
	}
	if !strings.HasPrefix(resp.ID, "msg_") {
		t.Fatalf("expected ID prefix msg_, got %q", resp.ID)
	}
	if *resp.StopReason != "end_turn" {
		t.Fatalf("expected stop_reason %q, got %q", "end_turn", *resp.StopReason)
	}

	if len(resp.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(resp.Content))
	}

	textBlock := resp.Content[0]
	if textBlock["type"] != "text" {
		t.Fatalf("expected block type %q, got %v", "text", textBlock["type"])
	}
	if textBlock["text"] != "Hello World" {
		t.Fatalf("expected text %q, got %v", "Hello World", textBlock["text"])
	}
}

func TestCollectAnthropicResponse_WithToolCalls(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "content", Content: "Let me check"},
		KiroEvent{
			Type: "tool_use",
			ToolUse: &ToolUseEvent{
				ID:        "toolu_abc",
				Name:      "get_weather",
				Arguments: `{"city":"Moscow"}`,
			},
		},
	)

	cfg := AnthropicStreamConfig{Model: "claude-sonnet-4"}
	resp, err := CollectAnthropicResponse(events, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if *resp.StopReason != "tool_use" {
		t.Fatalf("expected stop_reason %q, got %q", "tool_use", *resp.StopReason)
	}

	// Should have 2 content blocks: text + tool_use.
	if len(resp.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(resp.Content))
	}

	textBlock := resp.Content[0]
	if textBlock["type"] != "text" {
		t.Fatalf("expected first block type %q, got %v", "text", textBlock["type"])
	}

	toolBlock := resp.Content[1]
	if toolBlock["type"] != "tool_use" {
		t.Fatalf("expected second block type %q, got %v", "tool_use", toolBlock["type"])
	}
	if toolBlock["name"] != "get_weather" {
		t.Fatalf("expected tool name %q, got %v", "get_weather", toolBlock["name"])
	}
	if toolBlock["id"] != "toolu_abc" {
		t.Fatalf("expected tool ID %q, got %v", "toolu_abc", toolBlock["id"])
	}

	// Input should be parsed from JSON string to object.
	input, _ := toolBlock["input"].(map[string]any)
	if input["city"] != "Moscow" {
		t.Fatalf("expected city Moscow, got %v", input["city"])
	}
}

func TestCollectAnthropicResponse_ThinkingAsReasoning(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "thinking", ThinkingContent: "Let me think..."},
		KiroEvent{Type: "content", Content: "Answer"},
	)

	cfg := AnthropicStreamConfig{
		Model:            "claude-sonnet-4",
		ThinkingHandling: HandlingAsReasoning,
	}
	resp, err := CollectAnthropicResponse(events, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 2 blocks: thinking + text.
	if len(resp.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(resp.Content))
	}

	thinkingBlock := resp.Content[0]
	if thinkingBlock["type"] != "thinking" {
		t.Fatalf("expected first block type %q, got %v", "thinking", thinkingBlock["type"])
	}
	if thinkingBlock["thinking"] != "Let me think..." {
		t.Fatalf("expected thinking %q, got %v", "Let me think...", thinkingBlock["thinking"])
	}
	sig, _ := thinkingBlock["signature"].(string)
	if !strings.HasPrefix(sig, "sig_") {
		t.Fatalf("expected signature prefix sig_, got %q", sig)
	}

	textBlock := resp.Content[1]
	if textBlock["type"] != "text" {
		t.Fatalf("expected second block type %q, got %v", "text", textBlock["type"])
	}
	if textBlock["text"] != "Answer" {
		t.Fatalf("expected text %q, got %v", "Answer", textBlock["text"])
	}
}

func TestCollectAnthropicResponse_ThinkingPassMode(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "thinking", ThinkingContent: "thoughts"},
		KiroEvent{Type: "content", Content: " answer"},
	)

	cfg := AnthropicStreamConfig{
		Model:            "claude-sonnet-4",
		ThinkingHandling: HandlingPass,
	}
	resp, err := CollectAnthropicResponse(events, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// In pass mode, thinking is prepended to text content.
	if len(resp.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(resp.Content))
	}

	textBlock := resp.Content[0]
	text, _ := textBlock["text"].(string)
	if text != "thoughts answer" {
		t.Fatalf("expected text %q, got %q", "thoughts answer", text)
	}
}

func TestCollectAnthropicResponse_InvalidToolArguments(t *testing.T) {
	events := feedEvents(
		KiroEvent{
			Type: "tool_use",
			ToolUse: &ToolUseEvent{
				ID:        "toolu_abc",
				Name:      "func1",
				Arguments: "not valid json",
			},
		},
	)

	cfg := AnthropicStreamConfig{Model: "claude-sonnet-4"}
	resp, err := CollectAnthropicResponse(events, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(resp.Content))
	}

	toolBlock := resp.Content[0]
	input, _ := toolBlock["input"].(map[string]any)
	if len(input) != 0 {
		t.Fatalf("expected empty input for invalid JSON, got %v", input)
	}
}

func TestCollectAnthropicResponse_EmptyContent(t *testing.T) {
	// No content events, only an empty stream.
	events := feedEvents()

	cfg := AnthropicStreamConfig{Model: "claude-sonnet-4"}
	resp, err := CollectAnthropicResponse(events, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Content) != 0 {
		t.Fatalf("expected 0 content blocks for empty stream, got %d", len(resp.Content))
	}
}

func TestCollectAnthropicResponse_Error(t *testing.T) {
	events := feedEvents(
		KiroEvent{Type: "error", Error: &FirstTokenTimeoutError{}},
	)

	cfg := AnthropicStreamConfig{Model: "claude-sonnet-4"}
	_, err := CollectAnthropicResponse(events, cfg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if err.Error() != "first token timeout" {
		t.Fatalf("expected error %q, got %q", "first token timeout", err.Error())
	}
}

func TestCollectAnthropicResponse_ToolUseWithGeneratedID(t *testing.T) {
	events := feedEvents(
		KiroEvent{
			Type: "tool_use",
			ToolUse: &ToolUseEvent{
				Name:      "func1",
				Arguments: `{}`,
			},
		},
	)

	cfg := AnthropicStreamConfig{Model: "claude-sonnet-4"}
	resp, err := CollectAnthropicResponse(events, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(resp.Content))
	}

	toolBlock := resp.Content[0]
	id, _ := toolBlock["id"].(string)
	if !strings.HasPrefix(id, "toolu_") {
		t.Fatalf("expected generated tool ID with prefix toolu_, got %q", id)
	}
}
