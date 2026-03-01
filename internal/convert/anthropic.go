package convert

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/miltonparedes/giro/internal/types"
)

const (
	jsonNull           = "null"
	blockTypeText      = "text"
	blockTypeImage     = "image"
	blockToolResult    = "tool_result"
	sourceBase64       = "base64"
	contentEmptyResult = "(empty result)"
)

// AnthropicToCorePayload converts an Anthropic MessagesRequest to a Kiro API payload.
func AnthropicToCorePayload(
	req *types.AnthropicMessagesRequest,
	modelID, conversationID, profileARN string,
	cfg Config,
) (*KiroPayloadResult, error) {
	system := AnthropicSystemPrompt(req.System)
	unified := AnthropicMessages(req.Messages)
	tools := AnthropicTools(req.Tools)

	return BuildKiroPayload(
		system, unified, tools,
		modelID, conversationID, profileARN,
		cfg.FakeReasoning, cfg.FakeReasoningMaxTokens,
		cfg.TruncationRecovery, cfg.ToolDescriptionMaxLength,
	)
}

// AnthropicSystemPrompt extracts the system prompt from the Anthropic system field.
// It can be a string or an array of content blocks (with optional cache_control, ignored).
func AnthropicSystemPrompt(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == jsonNull {
		return ""
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}

	var parts []string
	for _, b := range blocks {
		if blockType, _ := b["type"].(string); blockType == blockTypeText {
			if text, _ := b[blockTypeText].(string); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// AnthropicMessages converts Anthropic messages to unified format.
// For assistant messages, tool_use blocks are extracted as ToolCalls and thinking blocks are skipped.
// For user messages, tool_result blocks become ToolResults and image blocks become Images.
func AnthropicMessages(messages []types.AnthropicMessage) []UnifiedMessage {
	result := make([]UnifiedMessage, 0, len(messages))

	for i := range messages {
		msg := &messages[i]
		um := convertSingleAnthropicMessage(msg)
		result = append(result, um)
	}

	return result
}

func convertSingleAnthropicMessage(msg *types.AnthropicMessage) UnifiedMessage {
	text := extractAnthropicText(msg.Content)

	um := UnifiedMessage{
		Role:    msg.Role,
		Content: text,
	}

	switch msg.Role {
	case roleAssistant:
		um.ToolCalls = extractAnthropicToolUses(msg.Content)
	case roleUser:
		um.ToolResults = extractAnthropicToolResults(msg.Content)
		um.Images = extractAnthropicImages(msg.Content)
		nestedImages := extractImagesFromAnthropicToolResults(msg.Content)
		um.Images = append(um.Images, nestedImages...)
	}

	return um
}

// extractAnthropicText extracts text content from Anthropic message content.
// Thinking blocks (type "thinking") are skipped.
func extractAnthropicText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == jsonNull {
		return ""
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}

	var parts []string
	for _, b := range blocks {
		if blockType, _ := b["type"].(string); blockType == blockTypeText {
			if text, _ := b[blockTypeText].(string); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "")
}

func extractAnthropicToolUses(raw json.RawMessage) []UnifiedToolCall {
	if len(raw) == 0 || string(raw) == jsonNull {
		return nil
	}

	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil
	}

	var calls []UnifiedToolCall
	for _, b := range blocks {
		if blockType, _ := b["type"].(string); blockType != "tool_use" {
			continue
		}

		id, _ := b["id"].(string)
		name, _ := b["name"].(string)
		if id == "" || name == "" {
			continue
		}

		args := marshalToolInput(b["input"])
		calls = append(calls, UnifiedToolCall{
			ID:        id,
			Name:      name,
			Arguments: args,
		})
	}

	return calls
}

func marshalToolInput(input any) string {
	if input == nil {
		return "{}"
	}
	if s, ok := input.(string); ok {
		return s
	}
	data, err := json.Marshal(input)
	if err != nil {
		slog.Warn("failed to marshal tool input", "error", err)
		return "{}"
	}
	return string(data)
}

func extractAnthropicToolResults(raw json.RawMessage) []UnifiedToolResult {
	if len(raw) == 0 || string(raw) == jsonNull {
		return nil
	}

	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil
	}

	var results []UnifiedToolResult
	for _, b := range blocks {
		if blockType, _ := b["type"].(string); blockType != blockToolResult {
			continue
		}

		toolUseID, _ := b["tool_use_id"].(string)
		if toolUseID == "" {
			continue
		}

		content := extractToolResultContent(b["content"])
		if content == "" {
			content = contentEmptyResult
		}

		results = append(results, UnifiedToolResult{
			ToolUseID: toolUseID,
			Content:   content,
		})
	}

	return results
}

func extractToolResultContent(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if blocks, ok := v.([]any); ok {
		var parts []string
		for _, item := range blocks {
			if m, ok := item.(map[string]any); ok {
				if t, _ := m["type"].(string); t == blockTypeText {
					if text, _ := m[blockTypeText].(string); text != "" {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}

func extractAnthropicImages(raw json.RawMessage) []UnifiedImage {
	if len(raw) == 0 || string(raw) == jsonNull {
		return nil
	}

	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil
	}

	var images []UnifiedImage
	for _, b := range blocks {
		if blockType, _ := b["type"].(string); blockType != blockTypeImage {
			continue
		}

		source, ok := b["source"].(map[string]any)
		if !ok {
			continue
		}

		sourceType, _ := source["type"].(string)
		if sourceType != sourceBase64 {
			continue
		}

		data, _ := source["data"].(string)
		mediaType, _ := source["media_type"].(string)
		if data == "" {
			continue
		}
		if mediaType == "" {
			mediaType = "image/jpeg"
		}

		images = append(images, UnifiedImage{
			MediaType: mediaType,
			Data:      data,
		})
	}

	return images
}

func extractImagesFromAnthropicToolResults(raw json.RawMessage) []UnifiedImage {
	if len(raw) == 0 || string(raw) == jsonNull {
		return nil
	}

	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil
	}

	var images []UnifiedImage
	for _, b := range blocks {
		if blockType, _ := b["type"].(string); blockType != blockToolResult {
			continue
		}

		contentRaw, ok := b["content"].([]any)
		if !ok {
			continue
		}

		for _, item := range contentRaw {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := m["type"].(string); t != blockTypeImage {
				continue
			}

			source, ok := m["source"].(map[string]any)
			if !ok {
				continue
			}
			if sourceType, _ := source["type"].(string); sourceType != sourceBase64 {
				continue
			}

			data, _ := source["data"].(string)
			mediaType, _ := source["media_type"].(string)
			if data == "" {
				continue
			}
			if mediaType == "" {
				mediaType = "image/jpeg"
			}

			images = append(images, UnifiedImage{
				MediaType: mediaType,
				Data:      data,
			})
		}
	}

	return images
}

// AnthropicTools converts Anthropic tool definitions to unified format.
func AnthropicTools(tools []types.AnthropicTool) []UnifiedTool {
	if len(tools) == 0 {
		return nil
	}

	unified := make([]UnifiedTool, 0, len(tools))
	for _, t := range tools {
		unified = append(unified, UnifiedTool{
			Name:        t.Name,
			Description: derefString(t.Description),
			InputSchema: t.InputSchema,
		})
	}
	return unified
}
