package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestChatCompletionRequest_Marshal(t *testing.T) {
	temp := 0.7
	req := ChatCompletionRequest{
		Model: "claude-sonnet-4",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"Hello"`)},
		},
		Stream:      true,
		Temperature: &temp,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got ChatCompletionRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Model != req.Model {
		t.Errorf("Model = %q, want %q", got.Model, req.Model)
	}
	if !got.Stream {
		t.Error("Stream should be true")
	}
	if got.Temperature == nil || *got.Temperature != 0.7 {
		t.Errorf("Temperature = %v, want 0.7", got.Temperature)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("Messages len = %d, want 1", len(got.Messages))
	}
}

func TestChatCompletionRequest_Unmarshal(t *testing.T) {
	input := `{
		"model": "claude-sonnet-4",
		"messages": [{"role": "user", "content": "Hi"}],
		"stream": false,
		"max_tokens": 100,
		"stop": ["\n"]
	}`

	var req ChatCompletionRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if req.Model != "claude-sonnet-4" {
		t.Errorf("Model = %q", req.Model)
	}
	if req.MaxTokens == nil || *req.MaxTokens != 100 {
		t.Errorf("MaxTokens = %v, want 100", req.MaxTokens)
	}
	if req.Stop == nil {
		t.Error("Stop should not be nil")
	}
}

func TestChatMessage_StringContent(t *testing.T) {
	input := `{"role": "user", "content": "hello world"}`

	var msg ChatMessage
	if err := json.Unmarshal([]byte(input), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if msg.Role != "user" {
		t.Errorf("Role = %q, want %q", msg.Role, "user")
	}

	var s string
	if err := json.Unmarshal(msg.Content, &s); err != nil {
		t.Fatalf("content unmarshal: %v", err)
	}
	if s != "hello world" {
		t.Errorf("Content = %q, want %q", s, "hello world")
	}
}

func TestChatMessage_ArrayContent(t *testing.T) {
	input := `{"role": "user", "content": [{"type": "text", "text": "describe this"}]}`

	var msg ChatMessage
	if err := json.Unmarshal([]byte(input), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	var blocks []map[string]interface{}
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		t.Fatalf("content unmarshal: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("blocks len = %d, want 1", len(blocks))
	}
	if blocks[0]["type"] != "text" {
		t.Errorf("block type = %v, want %q", blocks[0]["type"], "text")
	}
}

func TestChatMessage_NullContent(t *testing.T) {
	input := `{"role": "assistant", "content": null, "tool_calls": [{"id": "call_abc", "type": "function", "function": {"name": "get_weather", "arguments": "{}"}}]}`

	var msg ChatMessage
	if err := json.Unmarshal([]byte(input), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if msg.Role != "assistant" {
		t.Errorf("Role = %q, want %q", msg.Role, "assistant")
	}

	// null content unmarshals to nil RawMessage with no error
	var s *string
	if err := json.Unmarshal(msg.Content, &s); err != nil {
		t.Fatalf("content unmarshal: %v", err)
	}
	if s != nil {
		t.Error("content should unmarshal to nil pointer")
	}

	if len(msg.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(msg.ToolCalls))
	}
	if msg.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("tool call name = %q", msg.ToolCalls[0].Function.Name)
	}
}

func TestTool_StandardFormat(t *testing.T) {
	input := `{
		"type": "function",
		"function": {
			"name": "get_weather",
			"description": "Get current weather",
			"parameters": {"type": "object", "properties": {"city": {"type": "string"}}}
		}
	}`

	var tool Tool
	if err := json.Unmarshal([]byte(input), &tool); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if tool.Type != "function" {
		t.Errorf("Type = %q", tool.Type)
	}
	if tool.Function == nil {
		t.Fatal("Function should not be nil")
	}
	if tool.Function.Name != "get_weather" {
		t.Errorf("Function.Name = %q", tool.Function.Name)
	}
	if tool.Name != nil {
		t.Error("flat Name should be nil in standard format")
	}
}

func TestTool_FlatFormat(t *testing.T) {
	input := `{
		"type": "function",
		"name": "search",
		"description": "Search the web",
		"input_schema": {"type": "object", "properties": {"query": {"type": "string"}}}
	}`

	var tool Tool
	if err := json.Unmarshal([]byte(input), &tool); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if tool.Function != nil {
		t.Error("Function should be nil in flat format")
	}
	if tool.Name == nil || *tool.Name != "search" {
		t.Errorf("Name = %v, want %q", tool.Name, "search")
	}
	if tool.Description == nil || *tool.Description != "Search the web" {
		t.Errorf("Description = %v", tool.Description)
	}
	if tool.InputSchema == nil {
		t.Error("InputSchema should not be nil")
	}
}

func TestGenerateCompletionID(t *testing.T) {
	id := GenerateCompletionID()
	if !strings.HasPrefix(id, "chatcmpl-") {
		t.Errorf("prefix: got %q", id)
	}
	// "chatcmpl-" (9) + 32 hex = 41
	if len(id) != 41 {
		t.Errorf("len = %d, want 41", len(id))
	}

	id2 := GenerateCompletionID()
	if id == id2 {
		t.Error("IDs should be unique")
	}
}

func TestGenerateToolCallID(t *testing.T) {
	id := GenerateToolCallID()
	if !strings.HasPrefix(id, "call_") {
		t.Errorf("prefix: got %q", id)
	}
	// "call_" (5) + 8 hex = 13
	if len(id) != 13 {
		t.Errorf("len = %d, want 13", len(id))
	}

	id2 := GenerateToolCallID()
	if id == id2 {
		t.Error("IDs should be unique")
	}
}

func TestGenerateMessageID(t *testing.T) {
	id := GenerateMessageID()
	if !strings.HasPrefix(id, "msg_") {
		t.Errorf("prefix: got %q", id)
	}
	// "msg_" (4) + 24 hex = 28
	if len(id) != 28 {
		t.Errorf("len = %d, want 28", len(id))
	}

	id2 := GenerateMessageID()
	if id == id2 {
		t.Error("IDs should be unique")
	}
}

func TestGenerateToolUseID(t *testing.T) {
	id := GenerateToolUseID()
	if !strings.HasPrefix(id, "toolu_") {
		t.Errorf("prefix: got %q", id)
	}
	// "toolu_" (6) + 24 hex = 30
	if len(id) != 30 {
		t.Errorf("len = %d, want 30", len(id))
	}
}

func TestGenerateThinkingSignature(t *testing.T) {
	id := GenerateThinkingSignature()
	if !strings.HasPrefix(id, "sig_") {
		t.Errorf("prefix: got %q", id)
	}
	// "sig_" (4) + 32 hex = 36
	if len(id) != 36 {
		t.Errorf("len = %d, want 36", len(id))
	}
}
