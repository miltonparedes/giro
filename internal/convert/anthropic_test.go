package convert

import (
	"encoding/json"
	"testing"

	"github.com/miltonparedes/giro/internal/types"
)

func TestAnthropic_SystemString(t *testing.T) {
	raw := json.RawMessage(`"You are a helpful assistant."`)
	result := AnthropicSystemPrompt(raw)
	if result != "You are a helpful assistant." {
		t.Errorf("expected string system, got %q", result)
	}
}

func TestAnthropic_SystemBlocks(t *testing.T) {
	raw := json.RawMessage(`[
		{"type": "text", "text": "You are helpful.", "cache_control": {"type": "ephemeral"}},
		{"type": "text", "text": "Be concise."}
	]`)
	result := AnthropicSystemPrompt(raw)
	if result != "You are helpful.\nBe concise." {
		t.Errorf("expected joined system blocks, got %q", result)
	}
}

func TestAnthropic_SystemNull(t *testing.T) {
	result := AnthropicSystemPrompt(json.RawMessage(`null`))
	if result != "" {
		t.Errorf("expected empty for null system, got %q", result)
	}
}

func TestAnthropic_SystemEmpty(t *testing.T) {
	result := AnthropicSystemPrompt(nil)
	if result != "" {
		t.Errorf("expected empty for nil system, got %q", result)
	}
}

func TestAnthropic_ToolUseBlocks(t *testing.T) {
	content := `[
		{"type": "text", "text": "I'll check the weather."},
		{"type": "tool_use", "id": "toolu_abc123", "name": "get_weather", "input": {"city": "London"}}
	]`
	msgs := []types.AnthropicMessage{
		{Role: "assistant", Content: json.RawMessage(content)},
	}

	unified := AnthropicMessages(msgs)

	if len(unified) != 1 {
		t.Fatalf("expected 1 message, got %d", len(unified))
	}

	msg := unified[0]
	if msg.Content != "I'll check the weather." {
		t.Errorf("expected text content, got %q", msg.Content)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msg.ToolCalls))
	}

	tc := msg.ToolCalls[0]
	if tc.ID != "toolu_abc123" {
		t.Errorf("expected id 'toolu_abc123', got %q", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("expected name 'get_weather', got %q", tc.Name)
	}

	// Arguments should be JSON string of the input.
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
		t.Fatalf("failed to parse arguments: %v", err)
	}
	if args["city"] != "London" {
		t.Errorf("expected city=London, got %v", args["city"])
	}
}

func TestAnthropic_ToolResultBlocks(t *testing.T) {
	content := `[
		{"type": "tool_result", "tool_use_id": "toolu_abc123", "content": "Temperature: 15°C"}
	]`
	msgs := []types.AnthropicMessage{
		{Role: "user", Content: json.RawMessage(content)},
	}

	unified := AnthropicMessages(msgs)

	if len(unified) != 1 {
		t.Fatalf("expected 1 message, got %d", len(unified))
	}

	msg := unified[0]
	if len(msg.ToolResults) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(msg.ToolResults))
	}

	tr := msg.ToolResults[0]
	if tr.ToolUseID != "toolu_abc123" {
		t.Errorf("expected tool_use_id 'toolu_abc123', got %q", tr.ToolUseID)
	}
	if tr.Content != "Temperature: 15°C" {
		t.Errorf("expected content, got %q", tr.Content)
	}
}

func TestAnthropic_ToolResultBlocks_WithNestedImages(t *testing.T) {
	content := `[
		{
			"type": "tool_result",
			"tool_use_id": "toolu_screen",
			"content": [
				{"type": "text", "text": "Screenshot captured"},
				{
					"type": "image",
					"source": {
						"type": "base64",
						"media_type": "image/png",
						"data": "screendata123"
					}
				}
			]
		}
	]`
	msgs := []types.AnthropicMessage{
		{Role: "user", Content: json.RawMessage(content)},
	}

	unified := AnthropicMessages(msgs)
	msg := unified[0]

	if len(msg.ToolResults) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(msg.ToolResults))
	}
	if msg.ToolResults[0].Content != "Screenshot captured" {
		t.Errorf("expected text from nested content, got %q", msg.ToolResults[0].Content)
	}

	// Nested images should be extracted.
	if len(msg.Images) != 1 {
		t.Fatalf("expected 1 nested image, got %d", len(msg.Images))
	}
	if msg.Images[0].Data != "screendata123" {
		t.Errorf("expected image data, got %q", msg.Images[0].Data)
	}
	if msg.Images[0].MediaType != "image/png" {
		t.Errorf("expected image/png, got %q", msg.Images[0].MediaType)
	}
}

func TestAnthropic_ToolResultBlocks_EmptyContent(t *testing.T) {
	content := `[
		{"type": "tool_result", "tool_use_id": "toolu_1", "content": ""}
	]`
	msgs := []types.AnthropicMessage{
		{Role: "user", Content: json.RawMessage(content)},
	}

	unified := AnthropicMessages(msgs)
	if unified[0].ToolResults[0].Content != "(empty result)" {
		t.Errorf("expected '(empty result)', got %q", unified[0].ToolResults[0].Content)
	}
}

func TestAnthropic_ImageBlocks(t *testing.T) {
	content := `[
		{"type": "text", "text": "What's in this image?"},
		{
			"type": "image",
			"source": {
				"type": "base64",
				"media_type": "image/jpeg",
				"data": "/9j/4AAQtest"
			}
		}
	]`
	msgs := []types.AnthropicMessage{
		{Role: "user", Content: json.RawMessage(content)},
	}

	unified := AnthropicMessages(msgs)
	msg := unified[0]

	if msg.Content != "What's in this image?" {
		t.Errorf("expected text, got %q", msg.Content)
	}
	if len(msg.Images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(msg.Images))
	}
	if msg.Images[0].MediaType != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %q", msg.Images[0].MediaType)
	}
	if msg.Images[0].Data != "/9j/4AAQtest" {
		t.Errorf("expected base64 data, got %q", msg.Images[0].Data)
	}
}

func TestAnthropic_ThinkingBlocks(t *testing.T) {
	content := `[
		{"type": "thinking", "thinking": "Let me think about this..."},
		{"type": "text", "text": "The answer is 42."}
	]`
	msgs := []types.AnthropicMessage{
		{Role: "assistant", Content: json.RawMessage(content)},
	}

	unified := AnthropicMessages(msgs)
	msg := unified[0]

	// Thinking blocks should be skipped.
	if msg.Content != "The answer is 42." {
		t.Errorf("expected text only (thinking skipped), got %q", msg.Content)
	}
}

func TestAnthropic_StringContent(t *testing.T) {
	msgs := []types.AnthropicMessage{
		{Role: "user", Content: json.RawMessage(`"Hello world"`)},
	}

	unified := AnthropicMessages(msgs)
	if len(unified) != 1 {
		t.Fatalf("expected 1 message, got %d", len(unified))
	}
	if unified[0].Content != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", unified[0].Content)
	}
}

func TestAnthropic_ArrayContent(t *testing.T) {
	content := `[{"type": "text", "text": "Hello "}, {"type": "text", "text": "world"}]`
	msgs := []types.AnthropicMessage{
		{Role: "user", Content: json.RawMessage(content)},
	}

	unified := AnthropicMessages(msgs)
	if len(unified) != 1 {
		t.Fatalf("expected 1 message, got %d", len(unified))
	}
	if unified[0].Content != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", unified[0].Content)
	}
}

func TestAnthropic_NullContent(t *testing.T) {
	msgs := []types.AnthropicMessage{
		{Role: "user", Content: json.RawMessage(`null`)},
	}

	unified := AnthropicMessages(msgs)
	if unified[0].Content != "" {
		t.Errorf("expected empty for null, got %q", unified[0].Content)
	}
}

func TestAnthropic_Tools(t *testing.T) {
	desc := "Get weather for a city"
	tools := []types.AnthropicTool{
		{
			Name:        "get_weather",
			Description: &desc,
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"city": map[string]any{"type": "string"}},
			},
		},
	}

	unified := AnthropicTools(tools)
	if len(unified) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(unified))
	}
	if unified[0].Name != "get_weather" {
		t.Errorf("expected name 'get_weather', got %q", unified[0].Name)
	}
	if unified[0].Description != "Get weather for a city" {
		t.Errorf("expected description, got %q", unified[0].Description)
	}
}

func TestAnthropic_Tools_NilDescription(t *testing.T) {
	tools := []types.AnthropicTool{
		{
			Name:        "my_tool",
			Description: nil,
			InputSchema: map[string]any{"type": "object"},
		},
	}

	unified := AnthropicTools(tools)
	if unified[0].Description != "" {
		t.Errorf("expected empty description for nil, got %q", unified[0].Description)
	}
}

func TestAnthropic_Tools_Empty(t *testing.T) {
	unified := AnthropicTools(nil)
	if unified != nil {
		t.Errorf("expected nil for empty tools, got %v", unified)
	}
}

func TestAnthropic_FullConversion(t *testing.T) {
	desc := "Run a command"
	req := &types.AnthropicMessagesRequest{
		Model:     "claude-sonnet-4",
		MaxTokens: 1024,
		System:    json.RawMessage(`"You are a helpful assistant."`),
		Messages: []types.AnthropicMessage{
			{Role: "user", Content: json.RawMessage(`"Run ls"`)},
			{Role: "assistant", Content: json.RawMessage(`[
				{"type": "text", "text": "Running the command."},
				{"type": "tool_use", "id": "toolu_123", "name": "bash", "input": {"cmd": "ls"}}
			]`)},
			{Role: "user", Content: json.RawMessage(`[
				{"type": "tool_result", "tool_use_id": "toolu_123", "content": "file1.txt\nfile2.txt"}
			]`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type": "text", "text": "Found 2 files."}]`)},
			{Role: "user", Content: json.RawMessage(`"Thanks!"`)},
		},
		Tools: []types.AnthropicTool{
			{
				Name:        "bash",
				Description: &desc,
				InputSchema: map[string]any{"type": "object"},
			},
		},
	}

	cfg := Config{
		FakeReasoning:            false,
		ToolDescriptionMaxLength: 10000,
	}

	result, err := AnthropicToCorePayload(req, "claude-sonnet-4", "conv-456", "", cfg)
	if err != nil {
		t.Fatal(err)
	}

	convState := result.Payload["conversationState"].(map[string]any)
	if convState["conversationId"] != "conv-456" {
		t.Error("expected conversationId")
	}

	cm := convState["currentMessage"].(map[string]any)
	ui := cm["userInputMessage"].(map[string]any)
	if ui["content"] != "Thanks!" {
		t.Errorf("expected current content 'Thanks!', got %v", ui["content"])
	}

	hist, ok := convState["history"]
	if !ok {
		t.Fatal("expected history")
	}
	histArr := hist.([]map[string]any)
	if len(histArr) < 2 {
		t.Fatalf("expected at least 2 history entries, got %d", len(histArr))
	}
}

// --- marshalToolInput ---

func TestMarshalToolInput_Map(t *testing.T) {
	input := map[string]any{"city": "London"}
	result := marshalToolInput(input)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("expected valid JSON, got %q: %v", result, err)
	}
	if parsed["city"] != "London" {
		t.Errorf("expected city=London, got %v", parsed["city"])
	}
}

func TestMarshalToolInput_String(t *testing.T) {
	result := marshalToolInput(`{"already":"json"}`)
	if result != `{"already":"json"}` {
		t.Errorf("expected string passthrough, got %q", result)
	}
}

func TestMarshalToolInput_Nil(t *testing.T) {
	result := marshalToolInput(nil)
	if result != "{}" {
		t.Errorf("expected '{}' for nil, got %q", result)
	}
}
