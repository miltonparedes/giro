package convert

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- MergeAdjacentMessages ---

func TestMergeAdjacentMessages(t *testing.T) {
	msgs := []UnifiedMessage{
		{Role: "user", Content: "Hello"},
		{Role: "user", Content: "World"},
		{Role: "assistant", Content: "Hi"},
		{Role: "assistant", Content: "There"},
		{Role: "user", Content: "OK"},
	}
	merged := MergeAdjacentMessages(msgs)

	if len(merged) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(merged))
	}
	if merged[0].Content != "Hello\nWorld" {
		t.Errorf("expected merged user content 'Hello\\nWorld', got %q", merged[0].Content)
	}
	if merged[1].Content != "Hi\nThere" {
		t.Errorf("expected merged assistant content 'Hi\\nThere', got %q", merged[1].Content)
	}
	if merged[2].Content != "OK" {
		t.Errorf("expected last user content 'OK', got %q", merged[2].Content)
	}
}

func TestMergeAdjacentMessages_ToolCallsCombined(t *testing.T) {
	msgs := []UnifiedMessage{
		{Role: "assistant", Content: "A", ToolCalls: []UnifiedToolCall{{ID: "1", Name: "f1"}}},
		{Role: "assistant", Content: "B", ToolCalls: []UnifiedToolCall{{ID: "2", Name: "f2"}}},
	}
	merged := MergeAdjacentMessages(msgs)

	if len(merged) != 1 {
		t.Fatalf("expected 1 message, got %d", len(merged))
	}
	if len(merged[0].ToolCalls) != 2 {
		t.Errorf("expected 2 tool calls, got %d", len(merged[0].ToolCalls))
	}
}

func TestMergeAdjacentMessages_ToolResultsCombined(t *testing.T) {
	msgs := []UnifiedMessage{
		{Role: "user", Content: "A", ToolResults: []UnifiedToolResult{{ToolUseID: "1", Content: "r1"}}},
		{Role: "user", Content: "B", ToolResults: []UnifiedToolResult{{ToolUseID: "2", Content: "r2"}}},
	}
	merged := MergeAdjacentMessages(msgs)

	if len(merged) != 1 {
		t.Fatalf("expected 1 message, got %d", len(merged))
	}
	if len(merged[0].ToolResults) != 2 {
		t.Errorf("expected 2 tool results, got %d", len(merged[0].ToolResults))
	}
}

func TestMergeAdjacentMessages_Empty(t *testing.T) {
	merged := MergeAdjacentMessages(nil)
	if merged != nil {
		t.Errorf("expected nil, got %v", merged)
	}
}

// --- EnsureFirstMessageIsUser ---

func TestEnsureFirstMessageIsUser(t *testing.T) {
	msgs := []UnifiedMessage{
		{Role: "assistant", Content: "Hello"},
		{Role: "user", Content: "Hi"},
	}
	result := EnsureFirstMessageIsUser(msgs)

	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	if result[0].Role != "user" || result[0].Content != "(empty)" {
		t.Errorf("expected synthetic user '(empty)', got role=%q content=%q", result[0].Role, result[0].Content)
	}
}

func TestEnsureFirstMessageIsUser_AlreadyUser(t *testing.T) {
	msgs := []UnifiedMessage{
		{Role: "user", Content: "Hello"},
	}
	result := EnsureFirstMessageIsUser(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
}

func TestEnsureFirstMessageIsUser_Empty(t *testing.T) {
	result := EnsureFirstMessageIsUser(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

// --- NormalizeMessageRoles ---

func TestNormalizeMessageRoles(t *testing.T) {
	msgs := []UnifiedMessage{
		{Role: "developer", Content: "Context 1"},
		{Role: "system", Content: "Context 2"},
		{Role: "user", Content: "Question"},
		{Role: "assistant", Content: "Answer"},
	}
	result := NormalizeMessageRoles(msgs)

	expected := []string{"user", "user", "user", "assistant"}
	for i, msg := range result {
		if msg.Role != expected[i] {
			t.Errorf("message %d: expected role %q, got %q", i, expected[i], msg.Role)
		}
	}
}

func TestNormalizeMessageRoles_PreservesContent(t *testing.T) {
	msgs := []UnifiedMessage{
		{Role: "developer", Content: "system info", Images: []UnifiedImage{{MediaType: "image/png", Data: "abc"}}},
	}
	result := NormalizeMessageRoles(msgs)
	if result[0].Content != "system info" {
		t.Errorf("content not preserved: %q", result[0].Content)
	}
	if len(result[0].Images) != 1 {
		t.Errorf("images not preserved")
	}
}

// --- EnsureAlternatingRoles ---

func TestEnsureAlternatingRoles(t *testing.T) {
	msgs := []UnifiedMessage{
		{Role: "user", Content: "First"},
		{Role: "user", Content: "Second"},
		{Role: "user", Content: "Third"},
	}
	result := EnsureAlternatingRoles(msgs)

	if len(result) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(result))
	}
	expectedRoles := []string{"user", "assistant", "user", "assistant", "user"}
	for i, msg := range result {
		if msg.Role != expectedRoles[i] {
			t.Errorf("message %d: expected role %q, got %q", i, expectedRoles[i], msg.Role)
		}
	}
	if result[1].Content != "(empty)" || result[3].Content != "(empty)" {
		t.Errorf("synthetic assistant messages should have '(empty)' content")
	}
}

func TestEnsureAlternatingRoles_AlreadyAlternating(t *testing.T) {
	msgs := []UnifiedMessage{
		{Role: "user", Content: "A"},
		{Role: "assistant", Content: "B"},
		{Role: "user", Content: "C"},
	}
	result := EnsureAlternatingRoles(msgs)
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
}

func TestEnsureAlternatingRoles_SingleMessage(t *testing.T) {
	msgs := []UnifiedMessage{{Role: "user", Content: "A"}}
	result := EnsureAlternatingRoles(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
}

// --- StripAllToolContent ---

func TestStripAllToolContent(t *testing.T) {
	msgs := []UnifiedMessage{
		{
			Role:    "assistant",
			Content: "Thinking...",
			ToolCalls: []UnifiedToolCall{
				{ID: "call_123", Name: "bash", Arguments: `{"command": "ls"}`},
			},
		},
		{
			Role: "user",
			ToolResults: []UnifiedToolResult{
				{ToolUseID: "call_123", Content: "file1.txt\nfile2.txt"},
			},
		},
	}

	result := StripAllToolContent(msgs)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}

	// Assistant message should have tool call as text.
	if !strings.Contains(result[0].Content, "[Tool: bash (call_123)]") {
		t.Errorf("expected tool call text, got %q", result[0].Content)
	}
	if !strings.Contains(result[0].Content, `{"command": "ls"}`) {
		t.Errorf("expected arguments in text, got %q", result[0].Content)
	}
	if len(result[0].ToolCalls) != 0 {
		t.Errorf("expected no tool calls after stripping")
	}

	// User message should have tool result as text.
	if !strings.Contains(result[1].Content, "[Tool Result (call_123)]") {
		t.Errorf("expected tool result text, got %q", result[1].Content)
	}
	if !strings.Contains(result[1].Content, "file1.txt\nfile2.txt") {
		t.Errorf("expected result content in text, got %q", result[1].Content)
	}
	if len(result[1].ToolResults) != 0 {
		t.Errorf("expected no tool results after stripping")
	}
}

func TestStripAllToolContent_PreservesImages(t *testing.T) {
	imgs := []UnifiedImage{{MediaType: "image/png", Data: "abc"}}
	msgs := []UnifiedMessage{
		{
			Role:        "user",
			Content:     "Look at this",
			ToolResults: []UnifiedToolResult{{ToolUseID: "1", Content: "result"}},
			Images:      imgs,
		},
	}
	result := StripAllToolContent(msgs)
	if len(result[0].Images) != 1 {
		t.Errorf("expected images to be preserved")
	}
}

func TestStripAllToolContent_EmptyToolResult(t *testing.T) {
	msgs := []UnifiedMessage{
		{
			Role:        "user",
			ToolResults: []UnifiedToolResult{{ToolUseID: "1", Content: ""}},
		},
	}
	result := StripAllToolContent(msgs)
	if !strings.Contains(result[0].Content, "(empty result)") {
		t.Errorf("expected '(empty result)' for empty tool result, got %q", result[0].Content)
	}
}

// --- EnsureAssistantBeforeToolResults ---

func TestEnsureAssistantBeforeToolResults(t *testing.T) {
	msgs := []UnifiedMessage{
		{
			Role:        "user",
			Content:     "Here are results",
			ToolResults: []UnifiedToolResult{{ToolUseID: "call_1", Content: "output data"}},
		},
	}

	result := EnsureAssistantBeforeToolResults(msgs)

	if len(result) != 1 {
		t.Fatalf("expected 1 message (no synthetic assistant), got %d", len(result))
	}
	if result[0].Role != "user" {
		t.Errorf("expected user role, got %q", result[0].Role)
	}
	if !strings.Contains(result[0].Content, "[Tool Result (call_1)]") {
		t.Errorf("expected tool result converted to text, got %q", result[0].Content)
	}
	if !strings.Contains(result[0].Content, "output data") {
		t.Errorf("expected tool result content, got %q", result[0].Content)
	}
	if len(result[0].ToolResults) != 0 {
		t.Errorf("expected tool_results removed after conversion")
	}
}

func TestEnsureAssistantBeforeToolResults_WithPrecedingAssistant(t *testing.T) {
	msgs := []UnifiedMessage{
		{
			Role:      "assistant",
			Content:   "Using tool",
			ToolCalls: []UnifiedToolCall{{ID: "call_1", Name: "test"}},
		},
		{
			Role:        "user",
			ToolResults: []UnifiedToolResult{{ToolUseID: "call_1", Content: "result"}},
		},
	}

	result := EnsureAssistantBeforeToolResults(msgs)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	// Tool results should be preserved since preceding assistant has tool_calls.
	if len(result[1].ToolResults) != 1 {
		t.Errorf("expected tool results preserved, got %d", len(result[1].ToolResults))
	}
}

// --- Tool Description Truncation ---

func TestToolDescriptionTruncation(t *testing.T) {
	longDesc := strings.Repeat("x", 200)
	tools := []UnifiedTool{
		{Name: "short_tool", Description: "brief", InputSchema: map[string]any{"type": "object"}},
		{Name: "long_tool", Description: longDesc, InputSchema: map[string]any{"type": "object"}},
	}

	processed, doc := ProcessToolsWithLongDescriptions(tools, 100)

	if len(processed) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(processed))
	}

	// Short tool unchanged.
	if processed[0].Description != "brief" {
		t.Errorf("short tool description changed: %q", processed[0].Description)
	}

	// Long tool replaced with reference.
	if !strings.Contains(processed[1].Description, "## Tool: long_tool") {
		t.Errorf("expected reference description, got %q", processed[1].Description)
	}

	// Documentation generated.
	if !strings.Contains(doc, "# Tool Documentation") {
		t.Errorf("expected tool documentation header, got %q", doc)
	}
	if !strings.Contains(doc, "## Tool: long_tool") {
		t.Errorf("expected tool section in documentation, got %q", doc)
	}
	if !strings.Contains(doc, longDesc) {
		t.Errorf("expected full description in documentation")
	}
}

func TestToolDescriptionTruncation_Disabled(t *testing.T) {
	longDesc := strings.Repeat("x", 200)
	tools := []UnifiedTool{
		{Name: "long_tool", Description: longDesc},
	}

	processed, doc := ProcessToolsWithLongDescriptions(tools, 0)

	if len(processed) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(processed))
	}
	if processed[0].Description != longDesc {
		t.Errorf("expected description unchanged when limit=0")
	}
	if doc != "" {
		t.Errorf("expected empty documentation when limit=0, got %q", doc)
	}
}

func TestToolDescriptionTruncation_NilTools(t *testing.T) {
	processed, doc := ProcessToolsWithLongDescriptions(nil, 100)
	if processed != nil {
		t.Errorf("expected nil, got %v", processed)
	}
	if doc != "" {
		t.Errorf("expected empty doc, got %q", doc)
	}
}

func TestToolDescriptionTruncation_MultipleLong(t *testing.T) {
	tools := []UnifiedTool{
		{Name: "tool_a", Description: strings.Repeat("a", 200)},
		{Name: "tool_b", Description: strings.Repeat("b", 200)},
	}

	_, doc := ProcessToolsWithLongDescriptions(tools, 100)

	if !strings.Contains(doc, "## Tool: tool_a") {
		t.Errorf("expected tool_a in documentation")
	}
	if !strings.Contains(doc, "## Tool: tool_b") {
		t.Errorf("expected tool_b in documentation")
	}
	if !strings.Contains(doc, "---\n\n## Tool: tool_b") {
		t.Errorf("expected separator between tool docs")
	}
}

// --- Tool Name Validation ---

func TestToolNameValidation(t *testing.T) {
	tools := []UnifiedTool{
		{Name: "short_name"},
		{Name: strings.Repeat("a", 65)},
		{Name: strings.Repeat("b", 70)},
	}

	err := ValidateToolNames(tools)
	if err == nil {
		t.Fatal("expected error for long tool names")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "65 characters") {
		t.Errorf("expected first violation in error, got %q", errMsg)
	}
	if !strings.Contains(errMsg, "70 characters") {
		t.Errorf("expected second violation in error, got %q", errMsg)
	}
}

func TestToolNameValidation_AllValid(t *testing.T) {
	tools := []UnifiedTool{
		{Name: "valid_name"},
		{Name: strings.Repeat("x", 64)},
	}
	if err := ValidateToolNames(tools); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestToolNameValidation_NilTools(t *testing.T) {
	if err := ValidateToolNames(nil); err != nil {
		t.Errorf("expected no error for nil tools, got %v", err)
	}
}

// --- Empty Content Handling ---

func TestEmptyContentHandling(t *testing.T) {
	result, err := BuildKiroPayload(
		"",
		[]UnifiedMessage{{Role: "user", Content: ""}},
		nil,
		"model-1",
		"conv-1",
		"",
		false,
		0,
		false,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}

	current := extractCurrentContent(t, result.Payload)
	if current != "Continue" {
		t.Errorf("expected 'Continue' for empty content, got %q", current)
	}
}

func TestEmptyContentHandling_EmptyUser(t *testing.T) {
	msgs := []UnifiedMessage{
		{Role: "user", Content: ""},
		{Role: "assistant", Content: ""},
		{Role: "user", Content: "Hello"},
	}

	result, err := BuildKiroPayload("", msgs, nil, "m", "c", "", false, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Check history: first user should be "(empty)" after merge/normalize.
	hist := extractHistory(t, result.Payload)
	if len(hist) < 1 {
		t.Fatal("expected history")
	}
	firstUser := hist[0].(map[string]any)["userInputMessage"].(map[string]any)["content"].(string)
	if firstUser == "" {
		t.Error("empty user content should become non-empty in history")
	}
}

// --- System Prompt Injection ---

func TestSystemPromptInjection(t *testing.T) {
	msgs := []UnifiedMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "response"},
		{Role: "user", Content: "second"},
	}

	result, err := BuildKiroPayload("You are helpful.", msgs, nil, "m", "c", "", false, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}

	// System prompt should be prepended to the first user message in history.
	hist := extractHistory(t, result.Payload)
	if len(hist) == 0 {
		t.Fatal("expected history")
	}

	firstContent := hist[0].(map[string]any)["userInputMessage"].(map[string]any)["content"].(string)
	if !strings.HasPrefix(firstContent, "You are helpful.") {
		t.Errorf("expected system prompt at start of first user, got %q", firstContent)
	}
	if !strings.Contains(firstContent, "first") {
		t.Errorf("expected original content preserved, got %q", firstContent)
	}
}

func TestSystemPromptInjection_NoHistory(t *testing.T) {
	msgs := []UnifiedMessage{
		{Role: "user", Content: "Hello"},
	}

	result, err := BuildKiroPayload("System text.", msgs, nil, "m", "c", "", false, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}

	current := extractCurrentContent(t, result.Payload)
	if !strings.HasPrefix(current, "System text.") {
		t.Errorf("expected system prompt in current message, got %q", current)
	}
	if !strings.Contains(current, "Hello") {
		t.Errorf("expected original content preserved, got %q", current)
	}
}

// --- Thinking Tag Injection ---

func TestThinkingTagInjection(t *testing.T) {
	msgs := []UnifiedMessage{
		{Role: "user", Content: "What is 2+2?"},
	}

	result, err := BuildKiroPayload("", msgs, nil, "m", "c", "", true, 4000, false, 0)
	if err != nil {
		t.Fatal(err)
	}

	current := extractCurrentContent(t, result.Payload)
	if !strings.Contains(current, "<thinking_mode>enabled</thinking_mode>") {
		t.Errorf("expected thinking_mode tag, got %q", current)
	}
	if !strings.Contains(current, "<max_thinking_length>4000</max_thinking_length>") {
		t.Errorf("expected max_thinking_length tag, got %q", current)
	}
	if !strings.Contains(current, "<thinking_instruction>") {
		t.Errorf("expected thinking_instruction tag, got %q", current)
	}
	if !strings.Contains(current, "What is 2+2?") {
		t.Errorf("expected original content preserved, got %q", current)
	}
}

func TestThinkingTagInjection_Disabled(t *testing.T) {
	msgs := []UnifiedMessage{
		{Role: "user", Content: "What is 2+2?"},
	}

	result, err := BuildKiroPayload("", msgs, nil, "m", "c", "", false, 4000, false, 0)
	if err != nil {
		t.Fatal(err)
	}

	current := extractCurrentContent(t, result.Payload)
	if strings.Contains(current, "<thinking_mode>") {
		t.Errorf("thinking tags should not be injected when disabled")
	}
}

func TestThinkingTagInjection_SystemPromptAddition(t *testing.T) {
	msgs := []UnifiedMessage{
		{Role: "user", Content: "Hello"},
	}

	result, err := BuildKiroPayload("Base system.", msgs, nil, "m", "c", "", true, 4000, false, 0)
	if err != nil {
		t.Fatal(err)
	}

	current := extractCurrentContent(t, result.Payload)
	// System prompt should include thinking legitimization.
	if !strings.Contains(current, "Extended Thinking Mode") {
		t.Errorf("expected thinking legitimization in system prompt, got %q", current)
	}
}

// --- Assistant as Current Message ---

func TestAssistantAsCurrentMessage(t *testing.T) {
	msgs := []UnifiedMessage{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "I started saying..."},
	}

	result, err := BuildKiroPayload("", msgs, nil, "m", "c", "", false, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}

	current := extractCurrentContent(t, result.Payload)
	if current != "Continue" {
		t.Errorf("expected 'Continue' when last is assistant, got %q", current)
	}

	// Assistant message should be in history.
	hist := extractHistory(t, result.Payload)
	if len(hist) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(hist))
	}

	lastHist := hist[1].(map[string]any)
	if _, ok := lastHist["assistantResponseMessage"]; !ok {
		t.Error("expected assistant message pushed to history")
	}
}

// --- JSON Schema Sanitization ---

func TestJSONSchemaSanitization(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":                 "string",
				"additionalProperties": false,
			},
			"nested": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"inner": map[string]any{
						"type":                 "string",
						"additionalProperties": true,
					},
				},
				"required":             []any{},
				"additionalProperties": false,
			},
		},
		"required":             []any{"name"},
		"additionalProperties": false,
	}

	result := SanitizeJSONSchema(schema)

	// Top-level additionalProperties removed.
	if _, ok := result["additionalProperties"]; ok {
		t.Error("expected top-level additionalProperties removed")
	}

	// "required" with values preserved.
	if req, ok := result["required"]; !ok {
		t.Error("expected non-empty required preserved")
	} else {
		arr := req.([]any)
		if len(arr) != 1 || arr[0] != "name" {
			t.Errorf("unexpected required: %v", arr)
		}
	}

	// Nested properties sanitized.
	props := result["properties"].(map[string]any)
	nameSchema := props["name"].(map[string]any)
	if _, ok := nameSchema["additionalProperties"]; ok {
		t.Error("expected nested additionalProperties removed")
	}

	nestedSchema := props["nested"].(map[string]any)
	if _, ok := nestedSchema["additionalProperties"]; ok {
		t.Error("expected deeply nested additionalProperties removed")
	}
	if _, ok := nestedSchema["required"]; ok {
		t.Error("expected empty required removed from nested")
	}

	innerProps := nestedSchema["properties"].(map[string]any)
	innerSchema := innerProps["inner"].(map[string]any)
	if _, ok := innerSchema["additionalProperties"]; ok {
		t.Error("expected inner additionalProperties removed")
	}
}

func TestJSONSchemaSanitization_AnyOf(t *testing.T) {
	schema := map[string]any{
		"anyOf": []any{
			map[string]any{
				"type":                 "string",
				"additionalProperties": false,
			},
			map[string]any{
				"type":     "number",
				"required": []any{},
			},
		},
	}

	result := SanitizeJSONSchema(schema)
	anyOf := result["anyOf"].([]any)
	for _, item := range anyOf {
		m := item.(map[string]any)
		if _, ok := m["additionalProperties"]; ok {
			t.Error("expected additionalProperties removed from anyOf item")
		}
		if _, ok := m["required"]; ok {
			t.Error("expected empty required removed from anyOf item")
		}
	}
}

func TestJSONSchemaSanitization_NilSchema(t *testing.T) {
	result := SanitizeJSONSchema(nil)
	if len(result) != 0 {
		t.Errorf("expected empty map for nil schema, got %v", result)
	}
}

// --- BuildKiroPayload ---

func TestBuildKiroPayload_MinimalCase(t *testing.T) {
	result, err := BuildKiroPayload(
		"",
		[]UnifiedMessage{{Role: "user", Content: "Hello"}},
		nil,
		"claude-sonnet-4",
		"conv-123",
		"",
		false, 0, false, 0,
	)
	if err != nil {
		t.Fatal(err)
	}

	p := result.Payload
	convState := p["conversationState"].(map[string]any)

	if convState["chatTriggerType"] != "MANUAL" {
		t.Error("expected chatTriggerType MANUAL")
	}
	if convState["conversationId"] != "conv-123" {
		t.Error("expected conversationId conv-123")
	}

	cm := convState["currentMessage"].(map[string]any)
	ui := cm["userInputMessage"].(map[string]any)
	if ui["content"] != "Hello" {
		t.Errorf("expected content 'Hello', got %v", ui["content"])
	}
	if ui["modelId"] != "claude-sonnet-4" {
		t.Errorf("expected modelId 'claude-sonnet-4', got %v", ui["modelId"])
	}
	if ui["origin"] != "AI_EDITOR" {
		t.Errorf("expected origin 'AI_EDITOR', got %v", ui["origin"])
	}

	// No history.
	if _, ok := convState["history"]; ok {
		t.Error("expected no history key for single message")
	}

	// No profileArn.
	if _, ok := p["profileArn"]; ok {
		t.Error("expected no profileArn when empty")
	}
}

func TestBuildKiroPayload_WithHistory(t *testing.T) {
	msgs := []UnifiedMessage{
		{Role: "user", Content: "First question"},
		{Role: "assistant", Content: "First answer"},
		{Role: "user", Content: "Second question"},
	}

	result, err := BuildKiroPayload("", msgs, nil, "m", "c", "", false, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}

	hist := extractHistory(t, result.Payload)
	if len(hist) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(hist))
	}

	// First should be userInputMessage.
	first := hist[0].(map[string]any)
	if _, ok := first["userInputMessage"]; !ok {
		t.Error("expected first history entry to be userInputMessage")
	}

	// Second should be assistantResponseMessage.
	second := hist[1].(map[string]any)
	if _, ok := second["assistantResponseMessage"]; !ok {
		t.Error("expected second history entry to be assistantResponseMessage")
	}

	// Current should be the last user message.
	current := extractCurrentContent(t, result.Payload)
	if current != "Second question" {
		t.Errorf("expected 'Second question', got %q", current)
	}
}

func TestBuildKiroPayload_WithTools(t *testing.T) {
	tools := []UnifiedTool{
		{
			Name:        "get_weather",
			Description: "Get weather for a city",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string"},
				},
			},
		},
	}

	msgs := []UnifiedMessage{
		{Role: "user", Content: "What's the weather?"},
	}

	result, err := BuildKiroPayload("", msgs, tools, "m", "c", "", false, 0, false, 10000)
	if err != nil {
		t.Fatal(err)
	}

	cm := result.Payload["conversationState"].(map[string]any)["currentMessage"].(map[string]any)
	ui := cm["userInputMessage"].(map[string]any)
	ctx := ui["userInputMessageContext"].(map[string]any)
	toolsArr := ctx["tools"].([]map[string]any)

	if len(toolsArr) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(toolsArr))
	}

	spec := toolsArr[0]["toolSpecification"].(map[string]any)
	if spec["name"] != "get_weather" {
		t.Errorf("expected tool name 'get_weather', got %v", spec["name"])
	}
}

func TestBuildKiroPayload_WithToolResults(t *testing.T) {
	tools := []UnifiedTool{
		{Name: "bash", Description: "Run command", InputSchema: map[string]any{"type": "object"}},
	}

	msgs := []UnifiedMessage{
		{Role: "user", Content: "Run ls"},
		{
			Role:      "assistant",
			Content:   "Running...",
			ToolCalls: []UnifiedToolCall{{ID: "c1", Name: "bash", Arguments: `{"cmd":"ls"}`}},
		},
		{
			Role:        "user",
			Content:     "",
			ToolResults: []UnifiedToolResult{{ToolUseID: "c1", Content: "file1.txt"}},
		},
	}

	result, err := BuildKiroPayload("", msgs, tools, "m", "c", "", false, 0, false, 10000)
	if err != nil {
		t.Fatal(err)
	}

	// Current message should have tool results in context.
	cm := result.Payload["conversationState"].(map[string]any)["currentMessage"].(map[string]any)
	ui := cm["userInputMessage"].(map[string]any)
	ctx := ui["userInputMessageContext"].(map[string]any)

	toolResults := ctx["toolResults"].([]map[string]any)
	if len(toolResults) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(toolResults))
	}
	if toolResults[0]["toolUseId"] != "c1" {
		t.Errorf("expected toolUseId 'c1', got %v", toolResults[0]["toolUseId"])
	}
}

func TestBuildKiroPayload_WithImages(t *testing.T) {
	msgs := []UnifiedMessage{
		{
			Role:    "user",
			Content: "What's in this image?",
			Images:  []UnifiedImage{{MediaType: "image/png", Data: "iVBOR..."}},
		},
	}

	result, err := BuildKiroPayload("", msgs, nil, "m", "c", "", false, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}

	cm := result.Payload["conversationState"].(map[string]any)["currentMessage"].(map[string]any)
	ui := cm["userInputMessage"].(map[string]any)

	images := ui["images"].([]map[string]any)
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0]["format"] != "png" {
		t.Errorf("expected format 'png', got %v", images[0]["format"])
	}
	source := images[0]["source"].(map[string]any)
	if source["bytes"] != "iVBOR..." {
		t.Errorf("expected image bytes, got %v", source["bytes"])
	}

	// Images should NOT be in userInputMessageContext.
	if ctx, ok := ui["userInputMessageContext"]; ok {
		ctxMap := ctx.(map[string]any)
		if _, ok := ctxMap["images"]; ok {
			t.Error("images should NOT be in userInputMessageContext")
		}
	}
}

func TestBuildKiroPayload_ProfileArn(t *testing.T) {
	msgs := []UnifiedMessage{{Role: "user", Content: "Hello"}}

	// With profileArn.
	result, err := BuildKiroPayload("", msgs, nil, "m", "c", "arn:aws:codewhisperer:us-east-1:123:profile/abc", false, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Payload["profileArn"] != "arn:aws:codewhisperer:us-east-1:123:profile/abc" {
		t.Error("expected profileArn in payload")
	}

	// Without profileArn.
	result2, err := BuildKiroPayload("", msgs, nil, "m", "c", "", false, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result2.Payload["profileArn"]; ok {
		t.Error("expected no profileArn when empty")
	}
}

func TestBuildKiroPayload_HistoryOmitted(t *testing.T) {
	msgs := []UnifiedMessage{{Role: "user", Content: "Solo"}}

	result, err := BuildKiroPayload("", msgs, nil, "m", "c", "", false, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}

	convState := result.Payload["conversationState"].(map[string]any)
	if _, ok := convState["history"]; ok {
		t.Error("expected history key absent for single message")
	}
}

func TestBuildKiroPayload_NoMessages(t *testing.T) {
	_, err := BuildKiroPayload("", nil, nil, "m", "c", "", false, 0, false, 0)
	if err == nil {
		t.Fatal("expected error for no messages")
	}
	if !strings.Contains(err.Error(), "no messages") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuildKiroPayload_TruncationRecovery(t *testing.T) {
	msgs := []UnifiedMessage{{Role: "user", Content: "Hello"}}

	result, err := BuildKiroPayload("Base.", msgs, nil, "m", "c", "", false, 0, true, 0)
	if err != nil {
		t.Fatal(err)
	}

	current := extractCurrentContent(t, result.Payload)
	if !strings.Contains(current, "Output Truncation Handling") {
		t.Errorf("expected truncation recovery in system prompt, got %q", current)
	}
}

func TestBuildKiroPayload_EmptyDescriptionPlaceholder(t *testing.T) {
	tools := []UnifiedTool{
		{Name: "my_tool", Description: "", InputSchema: map[string]any{"type": "object"}},
	}
	msgs := []UnifiedMessage{{Role: "user", Content: "test"}}

	result, err := BuildKiroPayload("", msgs, tools, "m", "c", "", false, 0, false, 10000)
	if err != nil {
		t.Fatal(err)
	}

	cm := result.Payload["conversationState"].(map[string]any)["currentMessage"].(map[string]any)
	ui := cm["userInputMessage"].(map[string]any)
	ctx := ui["userInputMessageContext"].(map[string]any)
	toolsArr := ctx["tools"].([]map[string]any)
	spec := toolsArr[0]["toolSpecification"].(map[string]any)

	if spec["description"] != "Tool: my_tool" {
		t.Errorf("expected placeholder description 'Tool: my_tool', got %v", spec["description"])
	}
}

func TestBuildKiroPayload_HistoryImages(t *testing.T) {
	msgs := []UnifiedMessage{
		{
			Role:    "user",
			Content: "Look at this",
			Images:  []UnifiedImage{{MediaType: "image/jpeg", Data: "abc123"}},
		},
		{Role: "assistant", Content: "I see it"},
		{Role: "user", Content: "Now what?"},
	}

	result, err := BuildKiroPayload("", msgs, nil, "m", "c", "", false, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}

	hist := extractHistory(t, result.Payload)
	firstUser := hist[0].(map[string]any)["userInputMessage"].(map[string]any)
	imagesRaw := firstUser["images"].([]any)
	if len(imagesRaw) != 1 {
		t.Fatalf("expected 1 image in history, got %d", len(imagesRaw))
	}
	img0 := imagesRaw[0].(map[string]any)
	if img0["format"] != "jpeg" {
		t.Errorf("expected format 'jpeg', got %v", img0["format"])
	}
}

func TestBuildKiroPayload_HistoryToolUses(t *testing.T) {
	tools := []UnifiedTool{
		{Name: "bash", Description: "Run command", InputSchema: map[string]any{"type": "object"}},
	}

	msgs := []UnifiedMessage{
		{Role: "user", Content: "Run ls"},
		{
			Role:      "assistant",
			Content:   "Running...",
			ToolCalls: []UnifiedToolCall{{ID: "c1", Name: "bash", Arguments: `{"cmd":"ls"}`}},
		},
		{
			Role:        "user",
			ToolResults: []UnifiedToolResult{{ToolUseID: "c1", Content: "file1.txt"}},
		},
		{Role: "assistant", Content: "Found file1.txt"},
		{Role: "user", Content: "Thanks"},
	}

	result, err := BuildKiroPayload("", msgs, tools, "m", "c", "", false, 0, false, 10000)
	if err != nil {
		t.Fatal(err)
	}

	hist := extractHistory(t, result.Payload)

	// Second history entry should be assistant with toolUses.
	assistantEntry := hist[1].(map[string]any)["assistantResponseMessage"].(map[string]any)
	toolUsesRaw := assistantEntry["toolUses"].([]any)
	if len(toolUsesRaw) != 1 {
		t.Fatalf("expected 1 tool use, got %d", len(toolUsesRaw))
	}
	tu0 := toolUsesRaw[0].(map[string]any)
	if tu0["name"] != "bash" {
		t.Errorf("expected tool name 'bash', got %v", tu0["name"])
	}

	// Third history entry should be user with toolResults.
	userEntry := hist[2].(map[string]any)["userInputMessage"].(map[string]any)
	ctx := userEntry["userInputMessageContext"].(map[string]any)
	toolResultsRaw := ctx["toolResults"].([]any)
	if len(toolResultsRaw) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(toolResultsRaw))
	}
	tr0 := toolResultsRaw[0].(map[string]any)
	if tr0["toolUseId"] != "c1" {
		t.Errorf("expected toolUseId 'c1', got %v", tr0["toolUseId"])
	}
}

func TestBuildKiroPayload_DataURLImageStripping(t *testing.T) {
	msgs := []UnifiedMessage{
		{
			Role:    "user",
			Content: "Look",
			Images:  []UnifiedImage{{MediaType: "image/jpeg", Data: "data:image/png;base64,iVBOR"}},
		},
	}

	result, err := BuildKiroPayload("", msgs, nil, "m", "c", "", false, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}

	cm := result.Payload["conversationState"].(map[string]any)["currentMessage"].(map[string]any)
	ui := cm["userInputMessage"].(map[string]any)
	images := ui["images"].([]map[string]any)

	if images[0]["format"] != "png" {
		t.Errorf("expected format extracted from data URL, got %v", images[0]["format"])
	}
	source := images[0]["source"].(map[string]any)
	if source["bytes"] != "iVBOR" {
		t.Errorf("expected data URL prefix stripped, got %v", source["bytes"])
	}
}

// --- convertImagesToKiroFormat ---

func TestConvertImagesToKiroFormat_EmptyData(t *testing.T) {
	images := []UnifiedImage{{MediaType: "image/png", Data: ""}}
	result := convertImagesToKiroFormat(images)
	if len(result) != 0 {
		t.Errorf("expected empty result for empty data, got %d", len(result))
	}
}

// --- convertToolUsesToKiroFormat ---

func TestConvertToolUsesToKiroFormat_InvalidJSON(t *testing.T) {
	calls := []UnifiedToolCall{{ID: "1", Name: "test", Arguments: "invalid"}}
	result := convertToolUsesToKiroFormat(calls)
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	// Should fall back to empty map.
	input := result[0]["input"].(map[string]any)
	if len(input) != 0 {
		t.Errorf("expected empty map for invalid JSON, got %v", input)
	}
}

// --- Test helpers ---

func extractCurrentContent(t *testing.T, payload map[string]any) string {
	t.Helper()
	cs := payload["conversationState"].(map[string]any)
	cm := cs["currentMessage"].(map[string]any)
	ui := cm["userInputMessage"].(map[string]any)
	return ui["content"].(string)
}

func extractHistory(t *testing.T, payload map[string]any) []any {
	t.Helper()
	cs := payload["conversationState"].(map[string]any)
	hist, ok := cs["history"]
	if !ok {
		return nil
	}

	// Marshal and unmarshal to normalize types.
	data, err := json.Marshal(hist)
	if err != nil {
		t.Fatalf("failed to marshal history: %v", err)
	}
	var result []any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal history: %v", err)
	}
	return result
}
