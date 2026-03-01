package stream

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/miltonparedes/giro/internal/kiro"
	"github.com/miltonparedes/giro/internal/types"
)

// OpenAIStreamConfig controls how KiroEvents are formatted as OpenAI SSE.
type OpenAIStreamConfig struct {
	Model            string
	ThinkingHandling ThinkingHandling
}

// FormatOpenAISSE consumes events from the channel and returns a channel of
// SSE-formatted strings in the OpenAI chat.completion.chunk format.
func FormatOpenAISSE(events <-chan KiroEvent, cfg OpenAIStreamConfig) <-chan string {
	ch := make(chan string, 16)

	go func() {
		defer close(ch)

		completionID := types.GenerateCompletionID()
		created := time.Now().Unix()
		firstChunk := true
		var toolCalls []KiroEvent
		var usageData map[string]any

		roleChunk := buildOpenAIChunk(completionID, created, cfg.Model, map[string]any{"role": "assistant"}, nil, nil)
		ch <- formatOpenAIData(roleChunk)

		for evt := range events {
			switch evt.Type {
			case EventContent:
				delta := map[string]any{"content": evt.Content}
				if firstChunk {
					firstChunk = false
				}
				chunk := buildOpenAIChunk(completionID, created, cfg.Model, delta, nil, nil)
				ch <- formatOpenAIData(chunk)

			case EventThinking:
				emitOpenAIThinking(ch, completionID, created, cfg, evt, &firstChunk)

			case EventToolUse:
				toolCalls = append(toolCalls, evt)

			case EventUsage:
				usageData = evt.Usage

			case EventError:
				errChunk := buildOpenAIChunk(completionID, created, cfg.Model,
					map[string]any{"error": evt.Error.Error()}, nil, nil)
				ch <- formatOpenAIData(errChunk)
				return
			}
		}

		if len(toolCalls) > 0 {
			emitOpenAIToolCalls(ch, completionID, created, cfg.Model, toolCalls)
		}

		finishReason := FinishStop
		if len(toolCalls) > 0 {
			finishReason = FinishTools
		}
		emitOpenAIFinal(ch, completionID, created, cfg.Model, finishReason, usageData)

		ch <- "data: [DONE]\n\n"
	}()

	return ch
}

// emitOpenAIThinking sends a thinking event in the appropriate OpenAI format
// based on the configured handling mode.
func emitOpenAIThinking(
	ch chan<- string,
	completionID string,
	created int64,
	cfg OpenAIStreamConfig,
	evt KiroEvent,
	firstChunk *bool,
) {
	switch cfg.ThinkingHandling {
	case HandlingAsReasoning:
		delta := map[string]any{"reasoning_content": evt.ThinkingContent}
		if *firstChunk {
			*firstChunk = false
		}
		chunk := buildOpenAIChunk(completionID, created, cfg.Model, delta, nil, nil)
		ch <- formatOpenAIData(chunk)

	case HandlingPass, HandlingStripTags:
		// Already processed by ThinkingParser — emit as content.
		delta := map[string]any{"content": evt.ThinkingContent}
		if *firstChunk {
			*firstChunk = false
		}
		chunk := buildOpenAIChunk(completionID, created, cfg.Model, delta, nil, nil)
		ch <- formatOpenAIData(chunk)

	case HandlingRemove:
		// Discard thinking content.
	}
}

// emitOpenAIToolCalls sends a single chunk containing all tool calls with indices.
func emitOpenAIToolCalls(ch chan<- string, completionID string, created int64, model string, toolCalls []KiroEvent) {
	indexed := make([]map[string]any, 0, len(toolCalls))
	for i, tc := range toolCalls {
		if tc.ToolUse == nil {
			continue
		}
		indexed = append(indexed, map[string]any{
			"index": i,
			"id":    tc.ToolUse.ID,
			"type":  "function",
			"function": map[string]any{
				"name":      tc.ToolUse.Name,
				"arguments": tc.ToolUse.Arguments,
			},
		})
	}
	if len(indexed) == 0 {
		return
	}
	delta := map[string]any{"tool_calls": indexed}
	chunk := buildOpenAIChunk(completionID, created, model, delta, nil, nil)
	ch <- formatOpenAIData(chunk)
}

// emitOpenAIFinal sends the final chunk with finish_reason and usage.
func emitOpenAIFinal(
	ch chan<- string,
	completionID string,
	created int64,
	model string,
	finishReason string,
	usageData map[string]any,
) {
	usage := &types.ChatCompletionUsage{}
	chunk := buildOpenAIChunk(completionID, created, model, map[string]any{}, &finishReason, usage)

	// Attach credits_used if metering data is present.
	if usageData != nil {
		chunk["usage"].(map[string]any)["credits_used"] = usageData
	}

	ch <- formatOpenAIData(chunk)
}

// buildOpenAIChunk constructs a chat.completion.chunk JSON map.
func buildOpenAIChunk(
	id string,
	created int64,
	model string,
	delta map[string]any,
	finishReason *string,
	usage *types.ChatCompletionUsage,
) map[string]any {
	choice := map[string]any{
		"index":         0,
		"delta":         delta,
		"finish_reason": finishReason,
	}
	chunk := map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []map[string]any{choice},
	}
	if usage != nil {
		chunk["usage"] = map[string]any{
			"prompt_tokens":     usage.PromptTokens,
			"completion_tokens": usage.CompletionTokens,
			"total_tokens":      usage.TotalTokens,
		}
	}
	return chunk
}

// formatOpenAIData marshals data to JSON and wraps it in SSE format.
func formatOpenAIData(data any) string {
	b, err := json.Marshal(data)
	if err != nil {
		slog.Error("failed to marshal OpenAI chunk", "error", err)
		return ""
	}
	return "data: " + string(b) + "\n\n"
}

// openAICollected holds the accumulated state from consuming a KiroEvent channel.
type openAICollected struct {
	content               string
	reasoningContent      string
	toolCalls             []types.ToolCall
	usageData             map[string]any
	fullContentForBracket string
	lastErr               error
}

// collectOpenAIEvents drains the event channel into an openAICollected.
func collectOpenAIEvents(events <-chan KiroEvent) openAICollected {
	var c openAICollected
	for evt := range events {
		switch evt.Type {
		case EventContent:
			c.content += evt.Content
			c.fullContentForBracket += evt.Content
		case EventThinking:
			c.reasoningContent += evt.ThinkingContent
		case EventToolUse:
			if evt.ToolUse != nil {
				c.toolCalls = append(c.toolCalls, types.ToolCall{
					ID:   evt.ToolUse.ID,
					Type: "function",
					Function: types.ToolCallFunc{
						Name:      evt.ToolUse.Name,
						Arguments: evt.ToolUse.Arguments,
					},
				})
			}
		case EventUsage:
			c.usageData = evt.Usage
		case EventError:
			c.lastErr = evt.Error
		}
	}
	return c
}

// CollectOpenAIResponse consumes all events and builds a non-streaming
// ChatCompletionResponse.
func CollectOpenAIResponse(events <-chan KiroEvent, cfg OpenAIStreamConfig) (*types.ChatCompletionResponse, error) {
	completionID := types.GenerateCompletionID()
	created := time.Now().Unix()

	c := collectOpenAIEvents(events)
	if c.lastErr != nil {
		return nil, c.lastErr
	}

	for _, bc := range kiro.ParseBracketToolCalls(c.fullContentForBracket) {
		c.toolCalls = append(c.toolCalls, types.ToolCall{
			ID:   bc.ID,
			Type: "function",
			Function: types.ToolCallFunc{
				Name:      bc.Name,
				Arguments: bc.Arguments,
			},
		})
	}

	message := map[string]any{
		"role":    "assistant",
		"content": c.content,
	}

	switch cfg.ThinkingHandling {
	case HandlingAsReasoning:
		if c.reasoningContent != "" {
			message["reasoning_content"] = c.reasoningContent
		}
	case HandlingPass, HandlingStripTags:
		message["content"] = c.reasoningContent + c.content
	case HandlingRemove:
		// Discard.
	}

	if len(c.toolCalls) > 0 {
		message["tool_calls"] = c.toolCalls
	}

	finishReason := FinishStop
	if len(c.toolCalls) > 0 {
		finishReason = FinishTools
	}

	_ = c.usageData // TODO: incorporate metering when token counting is added

	return &types.ChatCompletionResponse{
		ID:      completionID,
		Object:  "chat.completion",
		Created: created,
		Model:   cfg.Model,
		Choices: []types.ChatCompletionChoice{
			{
				Index:        0,
				Message:      message,
				FinishReason: &finishReason,
			},
		},
		Usage: types.ChatCompletionUsage{},
	}, nil
}
