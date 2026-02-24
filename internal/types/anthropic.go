package types

import "encoding/json"

// AnthropicMessagesRequest is the request body for the Anthropic Messages API.
type AnthropicMessagesRequest struct {
	Model         string             `json:"model"`
	Messages      []AnthropicMessage `json:"messages"`
	MaxTokens     int                `json:"max_tokens"`
	System        json.RawMessage    `json:"system,omitempty"`
	Stream        bool               `json:"stream"`
	Tools         []AnthropicTool    `json:"tools,omitempty"`
	ToolChoice    json.RawMessage    `json:"tool_choice,omitempty"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	TopK          *int               `json:"top_k,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Metadata      json.RawMessage    `json:"metadata,omitempty"`
}

// AnthropicMessage represents a message in the Anthropic format.
type AnthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// AnthropicTool defines a tool in the Anthropic format.
type AnthropicTool struct {
	Name        string                 `json:"name"`
	Description *string                `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// AnthropicMessagesResponse is the non-streaming response from the Anthropic Messages API.
type AnthropicMessagesResponse struct {
	ID           string                   `json:"id"`
	Type         string                   `json:"type"`
	Role         string                   `json:"role"`
	Content      []map[string]interface{} `json:"content"`
	Model        string                   `json:"model"`
	StopReason   *string                  `json:"stop_reason"`
	StopSequence *string                  `json:"stop_sequence"`
	Usage        AnthropicUsage           `json:"usage"`
}

// AnthropicUsage holds token usage information for Anthropic responses.
type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
