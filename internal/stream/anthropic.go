package stream

import (
	"encoding/json"
	"log/slog"

	"github.com/miltonparedes/giro/internal/kiro"
	"github.com/miltonparedes/giro/internal/types"
)

// AnthropicStreamConfig controls how KiroEvents are formatted as Anthropic SSE.
type AnthropicStreamConfig struct {
	Model            string
	ThinkingHandling ThinkingHandling
}

// anthropicState tracks content block indices and open/close state during
// Anthropic SSE formatting.
type anthropicState struct {
	messageID         string
	blockIndex        int
	thinkingStarted   bool
	thinkingIndex     int
	textStarted       bool
	textIndex         int
	toolBlocks        []KiroEvent
	thinkingSignature string
}

// FormatAnthropicSSE consumes events from the channel and returns a channel of
// SSE-formatted strings in the Anthropic Messages streaming format.
func FormatAnthropicSSE(events <-chan KiroEvent, cfg AnthropicStreamConfig) <-chan string {
	ch := make(chan string, 16)

	go func() {
		defer close(ch)

		st := &anthropicState{
			messageID:         types.GenerateMessageID(),
			thinkingSignature: types.GenerateThinkingSignature(),
		}

		ch <- formatAnthropicEvent("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":            st.messageID,
				"type":          "message",
				"role":          "assistant",
				"content":       []any{},
				"model":         cfg.Model,
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage": map[string]any{
					"input_tokens":  0,
					"output_tokens": 0,
				},
			},
		})

		for evt := range events {
			switch evt.Type {
			case EventContent:
				emitAnthropicContent(ch, st, evt)
			case EventThinking:
				emitAnthropicThinking(ch, cfg, st, evt)
			case EventToolUse:
				st.toolBlocks = append(st.toolBlocks, evt)
			case EventError:
				ch <- formatAnthropicEvent("error", map[string]any{
					"type": "error",
					"error": map[string]any{
						"type":    "api_error",
						"message": evt.Error.Error(),
					},
				})
				return
			}
		}

		closeAnthropicTextBlock(ch, st)
		emitAnthropicToolBlocks(ch, st)

		stopReason := StopEndTurn
		if len(st.toolBlocks) > 0 {
			stopReason = StopToolUse
		}
		ch <- formatAnthropicEvent("message_delta", map[string]any{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason":   stopReason,
				"stop_sequence": nil,
			},
			"usage": map[string]any{
				"output_tokens": 0,
			},
		})

		ch <- formatAnthropicEvent("message_stop", map[string]any{
			"type": "message_stop",
		})
	}()

	return ch
}

// emitAnthropicContent handles a content event: closes any open thinking block,
// starts a text block if needed, and sends a text_delta.
func emitAnthropicContent(ch chan<- string, st *anthropicState, evt KiroEvent) {
	if st.thinkingStarted {
		ch <- formatAnthropicEvent("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": st.thinkingIndex,
		})
		st.thinkingStarted = false
		st.blockIndex++
	}

	if !st.textStarted {
		st.textIndex = st.blockIndex
		ch <- formatAnthropicEvent("content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": st.textIndex,
			"content_block": map[string]any{
				"type": "text",
				"text": "",
			},
		})
		st.textStarted = true
	}

	if evt.Content != "" {
		ch <- formatAnthropicEvent("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": st.textIndex,
			"delta": map[string]any{
				"type": "text_delta",
				"text": evt.Content,
			},
		})
	}
}

// emitAnthropicThinking handles a thinking event according to the configured mode.
func emitAnthropicThinking(ch chan<- string, cfg AnthropicStreamConfig, st *anthropicState, evt KiroEvent) {
	switch cfg.ThinkingHandling {
	case HandlingAsReasoning:
		emitAnthropicThinkingBlock(ch, st, evt)
	case HandlingPass, HandlingStripTags:
		// Already processed by ThinkingParser — emit as regular text content.
		emitAnthropicContent(ch, st, KiroEvent{Type: EventContent, Content: evt.ThinkingContent})
	case HandlingRemove:
		// Discard.
	}
}

// emitAnthropicThinkingBlock sends native Anthropic thinking content blocks.
func emitAnthropicThinkingBlock(ch chan<- string, st *anthropicState, evt KiroEvent) {
	if !st.thinkingStarted {
		st.thinkingIndex = st.blockIndex
		ch <- formatAnthropicEvent("content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": st.thinkingIndex,
			"content_block": map[string]any{
				"type":      "thinking",
				"thinking":  "",
				"signature": st.thinkingSignature,
			},
		})
		st.thinkingStarted = true
	}

	if evt.ThinkingContent != "" {
		ch <- formatAnthropicEvent("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": st.thinkingIndex,
			"delta": map[string]any{
				"type":     "thinking_delta",
				"thinking": evt.ThinkingContent,
			},
		})
	}
}

// closeAnthropicTextBlock closes the text content block if currently open.
func closeAnthropicTextBlock(ch chan<- string, st *anthropicState) {
	if st.textStarted {
		ch <- formatAnthropicEvent("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": st.textIndex,
		})
		st.textStarted = false
		st.blockIndex++
	}
}

// emitAnthropicToolBlocks sends content_block_start / input_json_delta /
// content_block_stop sequences for each accumulated tool call.
func emitAnthropicToolBlocks(ch chan<- string, st *anthropicState) {
	for _, tc := range st.toolBlocks {
		if tc.ToolUse == nil {
			continue
		}

		toolID := tc.ToolUse.ID
		if toolID == "" {
			toolID = types.GenerateToolUseID()
		}

		var inputObj any
		if err := json.Unmarshal([]byte(tc.ToolUse.Arguments), &inputObj); err != nil {
			inputObj = map[string]any{}
		}

		ch <- formatAnthropicEvent("content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": st.blockIndex,
			"content_block": map[string]any{
				"type":  EventToolUse,
				"id":    toolID,
				"name":  tc.ToolUse.Name,
				"input": map[string]any{},
			},
		})

		inputJSON, err := json.Marshal(inputObj)
		if err != nil {
			inputJSON = []byte("{}")
		}
		ch <- formatAnthropicEvent("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": st.blockIndex,
			"delta": map[string]any{
				"type":         "input_json_delta",
				"partial_json": string(inputJSON),
			},
		})

		ch <- formatAnthropicEvent("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": st.blockIndex,
		})

		st.blockIndex++
	}
}

// formatAnthropicEvent creates an Anthropic SSE string:
// "event: {type}\ndata: {json}\n\n".
func formatAnthropicEvent(eventType string, data any) string {
	b, err := json.Marshal(data)
	if err != nil {
		slog.Error("failed to marshal Anthropic event", "error", err)
		return ""
	}
	return "event: " + eventType + "\ndata: " + string(b) + "\n\n"
}

// anthropicCollected holds the accumulated state from consuming a KiroEvent channel
// for non-streaming Anthropic response building.
type anthropicCollected struct {
	content               string
	thinkingContent       string
	toolCalls             []KiroEvent
	fullContentForBracket string
	lastErr               error
}

// collectAnthropicEvents drains the event channel into an anthropicCollected.
func collectAnthropicEvents(events <-chan KiroEvent) anthropicCollected {
	var c anthropicCollected
	for evt := range events {
		switch evt.Type {
		case EventContent:
			c.content += evt.Content
			c.fullContentForBracket += evt.Content
		case EventThinking:
			c.thinkingContent += evt.ThinkingContent
		case EventToolUse:
			c.toolCalls = append(c.toolCalls, evt)
		case EventError:
			c.lastErr = evt.Error
		}
	}
	return c
}

// buildAnthropicToolBlock creates a tool_use content block from a KiroEvent.
func buildAnthropicToolBlock(tc KiroEvent) map[string]any {
	toolID := tc.ToolUse.ID
	if toolID == "" {
		toolID = types.GenerateToolUseID()
	}
	var inputObj any
	if err := json.Unmarshal([]byte(tc.ToolUse.Arguments), &inputObj); err != nil {
		inputObj = map[string]any{}
	}
	return map[string]any{
		"type":  EventToolUse,
		"id":    toolID,
		"name":  tc.ToolUse.Name,
		"input": inputObj,
	}
}

// CollectAnthropicResponse consumes all events and builds a non-streaming
// AnthropicMessagesResponse.
func CollectAnthropicResponse(events <-chan KiroEvent, cfg AnthropicStreamConfig) (*types.AnthropicMessagesResponse, error) {
	messageID := types.GenerateMessageID()

	c := collectAnthropicEvents(events)
	if c.lastErr != nil {
		return nil, c.lastErr
	}

	for _, bc := range kiro.ParseBracketToolCalls(c.fullContentForBracket) {
		c.toolCalls = append(c.toolCalls, KiroEvent{
			Type: EventToolUse,
			ToolUse: &ToolUseEvent{
				ID:        bc.ID,
				Name:      bc.Name,
				Arguments: bc.Arguments,
			},
		})
	}

	var blocks []map[string]any

	if c.thinkingContent != "" && cfg.ThinkingHandling == HandlingAsReasoning {
		blocks = append(blocks, map[string]any{
			"type":      "thinking",
			"thinking":  c.thinkingContent,
			"signature": types.GenerateThinkingSignature(),
		})
	}

	textContent := c.content
	if c.thinkingContent != "" && (cfg.ThinkingHandling == HandlingPass || cfg.ThinkingHandling == HandlingStripTags) {
		textContent = c.thinkingContent + c.content
	}
	if textContent != "" {
		blocks = append(blocks, map[string]any{
			"type": "text",
			"text": textContent,
		})
	}

	for _, tc := range c.toolCalls {
		if tc.ToolUse == nil {
			continue
		}
		blocks = append(blocks, buildAnthropicToolBlock(tc))
	}

	stopReason := StopEndTurn
	if len(c.toolCalls) > 0 {
		stopReason = StopToolUse
	}

	return &types.AnthropicMessagesResponse{
		ID:         messageID,
		Type:       "message",
		Role:       "assistant",
		Content:    blocks,
		Model:      cfg.Model,
		StopReason: &stopReason,
		Usage:      types.AnthropicUsage{},
	}, nil
}
