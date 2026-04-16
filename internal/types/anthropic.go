package types

import "encoding/json"

const (
	// AnthropicThinkingDisplaySummarized returns visible thinking content.
	AnthropicThinkingDisplaySummarized = "summarized"
	// AnthropicThinkingDisplayOmitted redacts thinking content but preserves continuity.
	AnthropicThinkingDisplayOmitted = "omitted"
)

// AnthropicMessagesRequest is the request body for the Anthropic Messages API.
type AnthropicMessagesRequest struct {
	Model         string             `json:"model"`
	Messages      []AnthropicMessage `json:"messages"`
	MaxTokens     int                `json:"max_tokens"`
	System        json.RawMessage    `json:"system,omitempty"`
	Stream        bool               `json:"stream"`
	Tools         []AnthropicTool    `json:"tools,omitempty"`
	ToolChoice    json.RawMessage    `json:"tool_choice,omitempty"`
	Thinking      *AnthropicThinking `json:"thinking,omitempty"`
	OutputConfig  *AnthropicOutput   `json:"output_config,omitempty"`
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

// AnthropicThinking configures extended thinking for Anthropic requests.
type AnthropicThinking struct {
	Type         string `json:"type,omitempty"`
	BudgetTokens *int   `json:"budget_tokens,omitempty"`
	Display      string `json:"display,omitempty"`
}

// Enabled reports whether thinking was explicitly requested.
func (t *AnthropicThinking) Enabled() bool {
	if t == nil {
		return false
	}

	switch t.Type {
	case "", "disabled":
		return false
	default:
		return true
	}
}

// DisplayMode returns the normalized thinking display mode.
func (t *AnthropicThinking) DisplayMode() string {
	if t != nil && t.Display == AnthropicThinkingDisplayOmitted {
		return AnthropicThinkingDisplayOmitted
	}

	return AnthropicThinkingDisplaySummarized
}

// MaxTokens returns the requested thinking budget or the provided fallback.
func (t *AnthropicThinking) MaxTokens(fallback int) int {
	if t != nil && t.BudgetTokens != nil && *t.BudgetTokens > 0 {
		return *t.BudgetTokens
	}

	return fallback
}

// AnthropicOutput holds Anthropic output configuration fields supported by giro.
type AnthropicOutput struct {
	Effort string `json:"effort,omitempty"`
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
