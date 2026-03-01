package types

import (
	"encoding/json"
	"testing"
)

func TestAnthropicMessagesRequest_Marshal(t *testing.T) {
	temp := 0.5
	req := AnthropicMessagesRequest{
		Model: "claude-sonnet-4",
		Messages: []AnthropicMessage{
			{Role: "user", Content: json.RawMessage(`"Hello"`)},
		},
		MaxTokens:   1024,
		Temperature: &temp,
		Stream:      true,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got AnthropicMessagesRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Model != req.Model {
		t.Errorf("Model = %q, want %q", got.Model, req.Model)
	}
	if got.MaxTokens != 1024 {
		t.Errorf("MaxTokens = %d, want 1024", got.MaxTokens)
	}
	if !got.Stream {
		t.Error("Stream should be true")
	}
	if got.Temperature == nil || *got.Temperature != 0.5 {
		t.Errorf("Temperature = %v, want 0.5", got.Temperature)
	}

	// Verify JSON field names
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("raw unmarshal: %v", err)
	}
	if _, ok := raw["max_tokens"]; !ok {
		t.Error("expected max_tokens key in JSON")
	}
}

func TestAnthropicMessage_StringContent(t *testing.T) {
	input := `{"role": "user", "content": "hello"}`

	var msg AnthropicMessage
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
	if s != "hello" {
		t.Errorf("Content = %q, want %q", s, "hello")
	}
}

func TestAnthropicMessage_ArrayContent(t *testing.T) {
	input := `{"role": "user", "content": [{"type": "text", "text": "analyze this"}, {"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "abc"}}]}`

	var msg AnthropicMessage
	if err := json.Unmarshal([]byte(input), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	var blocks []map[string]interface{}
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		t.Fatalf("content unmarshal: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("blocks len = %d, want 2", len(blocks))
	}
	if blocks[0]["type"] != "text" {
		t.Errorf("first block type = %v, want %q", blocks[0]["type"], "text")
	}
	if blocks[1]["type"] != "image" {
		t.Errorf("second block type = %v, want %q", blocks[1]["type"], "image")
	}
}

func TestAnthropicMessagesResponse_Marshal(t *testing.T) {
	stop := "end_turn"
	resp := AnthropicMessagesResponse{
		ID:   "msg_abc123",
		Type: "message",
		Role: "assistant",
		Content: []map[string]interface{}{
			{"type": "text", "text": "Hello!"},
		},
		Model:      "claude-sonnet-4",
		StopReason: &stop,
		Usage:      AnthropicUsage{InputTokens: 10, OutputTokens: 5},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got AnthropicMessagesResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.ID != resp.ID {
		t.Errorf("ID = %q, want %q", got.ID, resp.ID)
	}
	if got.StopReason == nil || *got.StopReason != "end_turn" {
		t.Errorf("StopReason = %v, want %q", got.StopReason, "end_turn")
	}
	if got.Usage.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", got.Usage.InputTokens)
	}
	if got.Usage.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", got.Usage.OutputTokens)
	}
}

func TestAnthropicTool_Marshal(t *testing.T) {
	desc := "Search the web"
	tool := AnthropicTool{
		Name:        "search",
		Description: &desc,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string"},
			},
		},
	}

	data, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("raw unmarshal: %v", err)
	}

	if _, ok := raw["input_schema"]; !ok {
		t.Error("expected input_schema key in JSON")
	}
}
