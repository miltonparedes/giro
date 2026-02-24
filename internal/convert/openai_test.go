package convert

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/miltonparedes/giro/internal/types"
)

// --- ExtractTextContent ---

func TestExtractTextContent_String(t *testing.T) {
	result := ExtractTextContent(json.RawMessage(`"Hello world"`))
	if result != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", result)
	}
}

func TestExtractTextContent_Array(t *testing.T) {
	content := `[{"type": "text", "text": "Hello "}, {"type": "text", "text": "world"}]`
	result := ExtractTextContent(json.RawMessage(content))
	if result != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", result)
	}
}

func TestExtractTextContent_Null(t *testing.T) {
	result := ExtractTextContent(json.RawMessage(`null`))
	if result != "" {
		t.Errorf("expected empty for null, got %q", result)
	}
}

func TestExtractTextContent_Empty(t *testing.T) {
	result := ExtractTextContent(nil)
	if result != "" {
		t.Errorf("expected empty for nil, got %q", result)
	}
}

func TestExtractTextContent_ArrayWithNonText(t *testing.T) {
	content := `[{"type": "image_url", "image_url": {"url": "data:img"}}, {"type": "text", "text": "Hello"}]`
	result := ExtractTextContent(json.RawMessage(content))
	if result != "Hello" {
		t.Errorf("expected 'Hello', got %q", result)
	}
}

// --- ExtractImagesFromContent ---

func TestExtractImagesFromContent_DataURL(t *testing.T) {
	content := `[{"type": "image_url", "image_url": {"url": "data:image/png;base64,iVBORtest"}}]`
	images := ExtractImagesFromContent(json.RawMessage(content))

	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].MediaType != "image/png" {
		t.Errorf("expected image/png, got %q", images[0].MediaType)
	}
	if images[0].Data != "iVBORtest" {
		t.Errorf("expected data, got %q", images[0].Data)
	}
}

func TestExtractImagesFromContent_AnthropicStyle(t *testing.T) {
	content := `[{
		"type": "image",
		"source": {"type": "base64", "media_type": "image/jpeg", "data": "/9j/test"}
	}]`
	images := ExtractImagesFromContent(json.RawMessage(content))

	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].MediaType != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %q", images[0].MediaType)
	}
	if images[0].Data != "/9j/test" {
		t.Errorf("expected data, got %q", images[0].Data)
	}
}

func TestExtractImagesFromContent_URLSkipped(t *testing.T) {
	content := `[{"type": "image_url", "image_url": {"url": "https://example.com/image.png"}}]`
	images := ExtractImagesFromContent(json.RawMessage(content))
	if len(images) != 0 {
		t.Errorf("expected URL-based images skipped, got %d", len(images))
	}
}

func TestExtractImagesFromContent_Empty(t *testing.T) {
	images := ExtractImagesFromContent(nil)
	if images != nil {
		t.Errorf("expected nil for nil input, got %v", images)
	}
}

func TestExtractImagesFromContent_NotArray(t *testing.T) {
	images := ExtractImagesFromContent(json.RawMessage(`"just a string"`))
	if images != nil {
		t.Errorf("expected nil for string input, got %v", images)
	}
}

func TestExtractImagesFromContent_AnthropicNoMediaType(t *testing.T) {
	content := `[{
		"type": "image",
		"source": {"type": "base64", "media_type": "", "data": "abc123"}
	}]`
	images := ExtractImagesFromContent(json.RawMessage(content))
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].MediaType != "image/jpeg" {
		t.Errorf("expected default media type image/jpeg, got %q", images[0].MediaType)
	}
}

// --- OpenAIMessages ---

func TestOpenAI_SystemExtraction(t *testing.T) {
	msgs := []types.ChatMessage{
		{Role: "system", Content: json.RawMessage(`"You are helpful."`)},
		{Role: "system", Content: json.RawMessage(`"Be concise."`)},
		{Role: "user", Content: json.RawMessage(`"Hello"`)},
	}

	system, unified := OpenAIMessages(msgs)

	if system != "You are helpful.\nBe concise." {
		t.Errorf("expected joined system prompt, got %q", system)
	}
	if len(unified) != 1 {
		t.Fatalf("expected 1 unified message (system excluded), got %d", len(unified))
	}
	if unified[0].Role != "user" || unified[0].Content != "Hello" {
		t.Errorf("unexpected message: role=%q content=%q", unified[0].Role, unified[0].Content)
	}
}

func TestOpenAI_ToolMessages(t *testing.T) {
	msgs := []types.ChatMessage{
		{Role: "user", Content: json.RawMessage(`"Use the tool"`)},
		{
			Role:    "assistant",
			Content: json.RawMessage(`""`),
			ToolCalls: []types.ToolCall{
				{ID: "call_abc", Type: "function", Function: types.ToolCallFunc{Name: "bash", Arguments: `{"cmd":"ls"}`}},
			},
		},
		{Role: "tool", Content: json.RawMessage(`"file1.txt\nfile2.txt"`), ToolCallID: strPtr("call_abc")},
		{Role: "user", Content: json.RawMessage(`"Thanks"`)},
	}

	_, unified := OpenAIMessages(msgs)

	if len(unified) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(unified))
	}

	// Third message should be the flushed tool result as user message.
	toolMsg := unified[2]
	if toolMsg.Role != "user" {
		t.Errorf("expected tool result as user, got %q", toolMsg.Role)
	}
	if len(toolMsg.ToolResults) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(toolMsg.ToolResults))
	}
	if toolMsg.ToolResults[0].ToolUseID != "call_abc" {
		t.Errorf("expected tool_use_id 'call_abc', got %q", toolMsg.ToolResults[0].ToolUseID)
	}
	if toolMsg.ToolResults[0].Content != "file1.txt\nfile2.txt" {
		t.Errorf("unexpected content: %q", toolMsg.ToolResults[0].Content)
	}
}

func TestOpenAI_ToolMessages_MultipleConsecutive(t *testing.T) {
	msgs := []types.ChatMessage{
		{
			Role:    "assistant",
			Content: json.RawMessage(`""`),
			ToolCalls: []types.ToolCall{
				{ID: "c1", Type: "function", Function: types.ToolCallFunc{Name: "f1", Arguments: "{}"}},
				{ID: "c2", Type: "function", Function: types.ToolCallFunc{Name: "f2", Arguments: "{}"}},
			},
		},
		{Role: "tool", Content: json.RawMessage(`"result1"`), ToolCallID: strPtr("c1")},
		{Role: "tool", Content: json.RawMessage(`"result2"`), ToolCallID: strPtr("c2")},
		{Role: "user", Content: json.RawMessage(`"Continue"`)},
	}

	_, unified := OpenAIMessages(msgs)

	// Multiple tool messages should be collected into a single user message.
	if len(unified) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(unified))
	}

	toolMsg := unified[1]
	if len(toolMsg.ToolResults) != 2 {
		t.Fatalf("expected 2 tool results in one message, got %d", len(toolMsg.ToolResults))
	}
}

func TestOpenAI_ToolMessages_AtEnd(t *testing.T) {
	msgs := []types.ChatMessage{
		{
			Role:    "assistant",
			Content: json.RawMessage(`""`),
			ToolCalls: []types.ToolCall{
				{ID: "c1", Type: "function", Function: types.ToolCallFunc{Name: "f1", Arguments: "{}"}},
			},
		},
		{Role: "tool", Content: json.RawMessage(`"result1"`), ToolCallID: strPtr("c1")},
	}

	_, unified := OpenAIMessages(msgs)

	// Tool results at the end should still be flushed.
	if len(unified) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(unified))
	}
	if len(unified[1].ToolResults) != 1 {
		t.Errorf("expected 1 tool result flushed at end, got %d", len(unified[1].ToolResults))
	}
}

func TestOpenAI_AssistantToolCalls(t *testing.T) {
	msgs := []types.ChatMessage{
		{
			Role:    "assistant",
			Content: json.RawMessage(`"Let me check."`),
			ToolCalls: []types.ToolCall{
				{
					ID:   "call_123",
					Type: "function",
					Function: types.ToolCallFunc{
						Name:      "get_weather",
						Arguments: `{"city":"London"}`,
					},
				},
			},
		},
	}

	_, unified := OpenAIMessages(msgs)

	if len(unified) != 1 {
		t.Fatalf("expected 1 message, got %d", len(unified))
	}

	msg := unified[0]
	if msg.Content != "Let me check." {
		t.Errorf("expected assistant content preserved, got %q", msg.Content)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_123" || tc.Name != "get_weather" || tc.Arguments != `{"city":"London"}` {
		t.Errorf("unexpected tool call: %+v", tc)
	}
}

func TestOpenAI_ImageExtraction(t *testing.T) {
	content := `[
		{"type": "text", "text": "What is this?"},
		{"type": "image_url", "image_url": {"url": "data:image/png;base64,iVBORtest"}}
	]`
	msgs := []types.ChatMessage{
		{Role: "user", Content: json.RawMessage(content)},
	}

	_, unified := OpenAIMessages(msgs)

	if len(unified) != 1 {
		t.Fatalf("expected 1 message, got %d", len(unified))
	}
	if unified[0].Content != "What is this?" {
		t.Errorf("expected text content, got %q", unified[0].Content)
	}
	if len(unified[0].Images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(unified[0].Images))
	}

	img := unified[0].Images[0]
	if img.MediaType != "image/png" {
		t.Errorf("expected media type 'image/png', got %q", img.MediaType)
	}
	if img.Data != "iVBORtest" {
		t.Errorf("expected data URL prefix stripped, got %q", img.Data)
	}
}

func TestOpenAI_ImageExtraction_JPEG(t *testing.T) {
	content := `[
		{"type": "image_url", "image_url": {"url": "data:image/jpeg;base64,/9j/4AAQ"}}
	]`
	msgs := []types.ChatMessage{
		{Role: "user", Content: json.RawMessage(content)},
	}

	_, unified := OpenAIMessages(msgs)
	if len(unified[0].Images) != 1 {
		t.Fatal("expected 1 image")
	}
	if unified[0].Images[0].MediaType != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %q", unified[0].Images[0].MediaType)
	}
	if unified[0].Images[0].Data != "/9j/4AAQ" {
		t.Errorf("expected base64 data, got %q", unified[0].Images[0].Data)
	}
}

func TestOpenAI_ImageExtraction_URLSkipped(t *testing.T) {
	content := `[
		{"type": "image_url", "image_url": {"url": "https://example.com/image.png"}}
	]`
	msgs := []types.ChatMessage{
		{Role: "user", Content: json.RawMessage(content)},
	}

	_, unified := OpenAIMessages(msgs)
	if len(unified[0].Images) != 0 {
		t.Errorf("expected non-data URLs to be skipped, got %d images", len(unified[0].Images))
	}
}

func TestOpenAI_ImageExtraction_ToolMessage(t *testing.T) {
	// Tool messages from MCP servers can contain images (screenshots).
	content := `[
		{"type": "text", "text": "Screenshot captured"},
		{"type": "image_url", "image_url": {"url": "data:image/png;base64,screendata"}}
	]`
	msgs := []types.ChatMessage{
		{Role: "tool", Content: json.RawMessage(content), ToolCallID: strPtr("call_1")},
		{Role: "user", Content: json.RawMessage(`"Continue"`)},
	}

	_, unified := OpenAIMessages(msgs)

	// Tool message should be flushed as user with both tool results and images.
	toolMsg := unified[0]
	if len(toolMsg.ToolResults) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(toolMsg.ToolResults))
	}
	if len(toolMsg.Images) != 1 {
		t.Fatalf("expected 1 image from tool message, got %d", len(toolMsg.Images))
	}
	if toolMsg.Images[0].Data != "screendata" {
		t.Errorf("expected image data, got %q", toolMsg.Images[0].Data)
	}
}

func TestOpenAI_FlatToolFormat(t *testing.T) {
	desc := "Get weather"
	name := "get_weather"
	tools := []types.Tool{
		{
			Type:        "function",
			Name:        &name,
			Description: &desc,
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"city": map[string]any{"type": "string"}},
			},
		},
	}

	unified := OpenAITools(tools)

	if len(unified) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(unified))
	}
	if unified[0].Name != "get_weather" {
		t.Errorf("expected name 'get_weather', got %q", unified[0].Name)
	}
	if unified[0].Description != "Get weather" {
		t.Errorf("expected description 'Get weather', got %q", unified[0].Description)
	}
	if unified[0].InputSchema == nil {
		t.Error("expected input_schema preserved")
	}
}

func TestOpenAI_StandardToolFormat(t *testing.T) {
	desc := "Get weather for a city"
	tools := []types.Tool{
		{
			Type: "function",
			Function: &types.ToolFunction{
				Name:        "get_weather",
				Description: &desc,
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{"city": map[string]any{"type": "string"}},
				},
			},
		},
	}

	unified := OpenAITools(tools)

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

func TestOpenAI_ToolFormat_SkipsNonFunction(t *testing.T) {
	tools := []types.Tool{
		{Type: "retrieval"},
	}
	unified := OpenAITools(tools)
	if unified != nil {
		t.Errorf("expected nil for non-function tools, got %v", unified)
	}
}

func TestOpenAI_ToolFormat_NilInput(t *testing.T) {
	unified := OpenAITools(nil)
	if unified != nil {
		t.Errorf("expected nil for nil tools, got %v", unified)
	}
}

func TestOpenAI_ToolFormat_NilDescription(t *testing.T) {
	tools := []types.Tool{
		{
			Type: "function",
			Function: &types.ToolFunction{
				Name:        "my_tool",
				Description: nil,
				Parameters:  map[string]any{"type": "object"},
			},
		},
	}
	unified := OpenAITools(tools)
	if len(unified) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(unified))
	}
	if unified[0].Description != "" {
		t.Errorf("expected empty description for nil, got %q", unified[0].Description)
	}
}

// --- ConvertOpenAIToCorePayload (integration) ---

func TestOpenAI_MixedConversation(t *testing.T) {
	desc := "Run a command"
	req := &types.ChatCompletionRequest{
		Model: "claude-sonnet-4",
		Messages: []types.ChatMessage{
			{Role: "system", Content: json.RawMessage(`"You are a helpful assistant."`)},
			{Role: "user", Content: json.RawMessage(`"Run ls"`)},
			{
				Role:    "assistant",
				Content: json.RawMessage(`"I'll run that for you."`),
				ToolCalls: []types.ToolCall{
					{ID: "call_1", Type: "function", Function: types.ToolCallFunc{Name: "bash", Arguments: `{"cmd":"ls"}`}},
				},
			},
			{Role: "tool", Content: json.RawMessage(`"file1.txt\nfile2.txt"`), ToolCallID: strPtr("call_1")},
			{Role: "user", Content: json.RawMessage(`"Great, thanks!"`)},
		},
		Tools: []types.Tool{
			{
				Type: "function",
				Function: &types.ToolFunction{
					Name:        "bash",
					Description: &desc,
					Parameters:  map[string]any{"type": "object", "properties": map[string]any{"cmd": map[string]any{"type": "string"}}},
				},
			},
		},
	}

	cfg := Config{
		FakeReasoning:            false,
		ToolDescriptionMaxLength: 10000,
	}

	result, err := OpenAIToCorePayload(req, "claude-sonnet-4", "conv-123", "", cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Verify payload structure.
	convState := result.Payload["conversationState"].(map[string]any)
	if convState["conversationId"] != "conv-123" {
		t.Error("expected conversationId")
	}

	// Current message should be the last user message.
	cm := convState["currentMessage"].(map[string]any)
	ui := cm["userInputMessage"].(map[string]any)
	currentContent, _ := ui["content"].(string)
	if !strings.Contains(currentContent, "Great, thanks!") {
		t.Errorf("expected current content to contain 'Great, thanks!', got %q", currentContent)
	}

	// Should have history.
	hist, ok := convState["history"]
	if !ok {
		t.Fatal("expected history")
	}
	histArr := hist.([]map[string]any)
	if len(histArr) < 2 {
		t.Fatalf("expected at least 2 history entries, got %d", len(histArr))
	}
}

func TestOpenAI_StringContent(t *testing.T) {
	msgs := []types.ChatMessage{
		{Role: "user", Content: json.RawMessage(`"Hello world"`)},
	}

	_, unified := OpenAIMessages(msgs)
	if len(unified) != 1 {
		t.Fatalf("expected 1 message, got %d", len(unified))
	}
	if unified[0].Content != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", unified[0].Content)
	}
}

func TestOpenAI_ArrayContent(t *testing.T) {
	content := `[{"type": "text", "text": "Hello "}, {"type": "text", "text": "world"}]`
	msgs := []types.ChatMessage{
		{Role: "user", Content: json.RawMessage(content)},
	}

	_, unified := OpenAIMessages(msgs)
	if len(unified) != 1 {
		t.Fatalf("expected 1 message, got %d", len(unified))
	}
	if unified[0].Content != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", unified[0].Content)
	}
}

func TestOpenAI_NullContent(t *testing.T) {
	msgs := []types.ChatMessage{
		{Role: "assistant", Content: json.RawMessage(`null`)},
	}

	_, unified := OpenAIMessages(msgs)
	if len(unified) != 1 {
		t.Fatalf("expected 1 message, got %d", len(unified))
	}
	if unified[0].Content != "" {
		t.Errorf("expected empty string for null content, got %q", unified[0].Content)
	}
}

func TestOpenAI_EmptyContent(t *testing.T) {
	msgs := []types.ChatMessage{
		{Role: "user", Content: nil},
	}

	_, unified := OpenAIMessages(msgs)
	if len(unified) != 1 {
		t.Fatalf("expected 1 message, got %d", len(unified))
	}
	if unified[0].Content != "" {
		t.Errorf("expected empty string for nil content, got %q", unified[0].Content)
	}
}

func TestOpenAI_EmptyToolResult(t *testing.T) {
	msgs := []types.ChatMessage{
		{Role: "tool", Content: json.RawMessage(`""`), ToolCallID: strPtr("c1")},
	}

	_, unified := OpenAIMessages(msgs)
	if len(unified) != 1 {
		t.Fatalf("expected 1 message, got %d", len(unified))
	}
	if unified[0].ToolResults[0].Content != "(empty result)" {
		t.Errorf("expected '(empty result)', got %q", unified[0].ToolResults[0].Content)
	}
}

// --- parseDataURL ---

func TestParseDataURL_Valid(t *testing.T) {
	img := parseDataURL("data:image/png;base64,abc123")
	if img == nil {
		t.Fatal("expected non-nil image")
	}
	if img.MediaType != "image/png" {
		t.Errorf("expected image/png, got %q", img.MediaType)
	}
	if img.Data != "abc123" {
		t.Errorf("expected abc123, got %q", img.Data)
	}
}

func TestParseDataURL_NotDataURL(t *testing.T) {
	img := parseDataURL("https://example.com/img.png")
	if img != nil {
		t.Errorf("expected nil for non-data URL, got %+v", img)
	}
}

func TestParseDataURL_EmptyData(t *testing.T) {
	img := parseDataURL("data:image/png;base64,")
	if img != nil {
		t.Errorf("expected nil for empty data, got %+v", img)
	}
}

// --- helpers ---

func strPtr(s string) *string {
	return &s
}
