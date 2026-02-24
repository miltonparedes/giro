package convert

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/miltonparedes/giro/internal/types"
)

// Config holds configuration for the conversion pipeline.
type Config struct {
	FakeReasoning            bool
	FakeReasoningMaxTokens   int
	TruncationRecovery       bool
	ToolDescriptionMaxLength int
}

// OpenAIToCorePayload converts an OpenAI ChatCompletionRequest to a Kiro API payload.
func OpenAIToCorePayload(
	req *types.ChatCompletionRequest,
	modelID, conversationID, profileARN string,
	cfg Config,
) (*KiroPayloadResult, error) {
	system, unified := OpenAIMessages(req.Messages)
	tools := OpenAITools(req.Tools)

	return BuildKiroPayload(
		system, unified, tools,
		modelID, conversationID, profileARN,
		cfg.FakeReasoning, cfg.FakeReasoningMaxTokens,
		cfg.TruncationRecovery, cfg.ToolDescriptionMaxLength,
	)
}

// ExtractTextContent extracts plain text from an OpenAI polymorphic content field.
// If content is null/empty, returns "". If content is a JSON string, returns the string.
// If content is a JSON array, joins text from blocks with type "text".
func ExtractTextContent(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == jsonNull {
		return ""
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err == nil {
		return extractTextFromBlocks(blocks)
	}

	return ""
}

// extractTextFromBlocks joins text from content blocks with type "text".
func extractTextFromBlocks(blocks []map[string]any) string {
	var parts []string
	for _, b := range blocks {
		if blockType, _ := b["type"].(string); blockType == "text" {
			if text, _ := b["text"].(string); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "")
}

// ExtractImagesFromContent extracts base64-encoded images from an OpenAI content array.
// Supports OpenAI image_url blocks (data URLs only) and Anthropic-style image blocks.
// URL-based images are skipped with a log warning.
func ExtractImagesFromContent(raw json.RawMessage) []UnifiedImage {
	if len(raw) == 0 || string(raw) == jsonNull {
		return nil
	}

	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil
	}

	var images []UnifiedImage
	for _, b := range blocks {
		blockType, _ := b["type"].(string)
		switch blockType {
		case "image_url":
			if img := extractOpenAIImageURL(b); img != nil {
				images = append(images, *img)
			}
		case "image":
			if img := extractAnthropicStyleImage(b); img != nil {
				images = append(images, *img)
			}
		}
	}

	return images
}

func extractOpenAIImageURL(block map[string]any) *UnifiedImage {
	imageURL, ok := block["image_url"].(map[string]any)
	if !ok {
		return nil
	}

	url, _ := imageURL["url"].(string)
	if url == "" {
		return nil
	}

	img := parseDataURL(url)
	if img != nil {
		return img
	}

	slog.Warn("skipping non-data-URL image (not supported by Kiro)",
		"url_prefix", truncateStr(url, 50))
	return nil
}

func extractAnthropicStyleImage(block map[string]any) *UnifiedImage {
	source, ok := block["source"].(map[string]any)
	if !ok {
		return nil
	}

	sourceType, _ := source["type"].(string)
	if sourceType != "base64" {
		return nil
	}

	data, _ := source["data"].(string)
	mediaType, _ := source["media_type"].(string)
	if data == "" {
		return nil
	}
	if mediaType == "" {
		mediaType = "image/jpeg"
	}

	return &UnifiedImage{
		MediaType: mediaType,
		Data:      data,
	}
}

// parseDataURL parses a data URL (data:image/jpeg;base64,...) into a UnifiedImage.
// Returns nil if the URL is not a data URL or has no data.
func parseDataURL(url string) *UnifiedImage {
	if !strings.HasPrefix(url, "data:") {
		return nil
	}

	commaIdx := strings.Index(url, ",")
	if commaIdx < 0 {
		return nil
	}

	header := url[:commaIdx]
	data := url[commaIdx+1:]
	if data == "" {
		return nil
	}

	mediaType := "image/jpeg"
	mediaPart := strings.SplitN(header, ";", 2)[0]
	if extracted := strings.TrimPrefix(mediaPart, "data:"); extracted != "" {
		mediaType = extracted
	}

	return &UnifiedImage{
		MediaType: mediaType,
		Data:      data,
	}
}

// OpenAIMessages converts OpenAI ChatMessage list to a system prompt and unified messages.
// System messages are extracted and joined. Tool messages are collected and flushed
// as a single user message with ToolResults when the next non-tool message appears.
func OpenAIMessages(messages []types.ChatMessage) (string, []UnifiedMessage) {
	var systemParts []string
	var nonSystem []types.ChatMessage

	for i := range messages {
		if messages[i].Role == "system" {
			systemParts = append(systemParts, ExtractTextContent(messages[i].Content))
		} else {
			nonSystem = append(nonSystem, messages[i])
		}
	}
	system := strings.TrimSpace(strings.Join(systemParts, "\n"))

	var result []UnifiedMessage
	var pendingToolResults []UnifiedToolResult
	var pendingToolImages []UnifiedImage

	for i := range nonSystem {
		msg := &nonSystem[i]

		if msg.Role == "tool" {
			pendingToolResults, pendingToolImages = collectOpenAIToolMessage(
				msg, pendingToolResults, pendingToolImages,
			)
			continue
		}

		if len(pendingToolResults) > 0 {
			result = append(result, UnifiedMessage{
				Role:        roleUser,
				ToolResults: pendingToolResults,
				Images:      pendingToolImages,
			})
			pendingToolResults = nil
			pendingToolImages = nil
		}

		result = append(result, convertOpenAINonToolMessage(msg))
	}

	if len(pendingToolResults) > 0 {
		result = append(result, UnifiedMessage{
			Role:        roleUser,
			ToolResults: pendingToolResults,
			Images:      pendingToolImages,
		})
	}

	return system, result
}

func collectOpenAIToolMessage(
	msg *types.ChatMessage,
	pending []UnifiedToolResult,
	pendingImages []UnifiedImage,
) ([]UnifiedToolResult, []UnifiedImage) {
	content := ExtractTextContent(msg.Content)
	if content == "" {
		content = contentEmptyResult
	}

	pending = append(pending, UnifiedToolResult{
		ToolUseID: derefString(msg.ToolCallID),
		Content:   content,
	})

	if imgs := ExtractImagesFromContent(msg.Content); len(imgs) > 0 {
		pendingImages = append(pendingImages, imgs...)
	}

	return pending, pendingImages
}

func convertOpenAINonToolMessage(msg *types.ChatMessage) UnifiedMessage {
	um := UnifiedMessage{
		Role:    msg.Role,
		Content: ExtractTextContent(msg.Content),
	}

	switch msg.Role {
	case roleAssistant:
		um.ToolCalls = extractOpenAIToolCalls(msg.ToolCalls)
	case roleUser:
		um.ToolResults = extractToolResultsFromOpenAIContent(msg.Content)
		um.Images = ExtractImagesFromContent(msg.Content)
	}

	return um
}

// extractOpenAIToolCalls converts OpenAI tool calls to unified format.
func extractOpenAIToolCalls(calls []types.ToolCall) []UnifiedToolCall {
	if len(calls) == 0 {
		return nil
	}

	unified := make([]UnifiedToolCall, 0, len(calls))
	for _, tc := range calls {
		args := tc.Function.Arguments
		if args == "" {
			args = "{}"
		}
		unified = append(unified, UnifiedToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}
	return unified
}

// extractToolResultsFromOpenAIContent extracts tool_result blocks from OpenAI user content.
func extractToolResultsFromOpenAIContent(raw json.RawMessage) []UnifiedToolResult {
	if len(raw) == 0 || string(raw) == jsonNull {
		return nil
	}

	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil
	}

	var results []UnifiedToolResult
	for _, b := range blocks {
		if blockType, _ := b["type"].(string); blockType != "tool_result" {
			continue
		}

		toolUseID, _ := b["tool_use_id"].(string)
		content := extractAnyContent(b["content"])
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

// extractAnyContent extracts text from content that can be a string or list of blocks.
func extractAnyContent(v any) string {
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
				if t, _ := m["type"].(string); t == "text" {
					if text, _ := m["text"].(string); text != "" {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}

// OpenAITools converts OpenAI tool definitions to unified format.
// Supports both standard nested format (Function field) and flat Cursor-style format (Name field).
func OpenAITools(tools []types.Tool) []UnifiedTool {
	if len(tools) == 0 {
		return nil
	}

	var unified []UnifiedTool
	for _, t := range tools {
		if t.Type != "function" {
			continue
		}

		switch {
		case t.Function != nil:
			unified = append(unified, UnifiedTool{
				Name:        t.Function.Name,
				Description: derefString(t.Function.Description),
				InputSchema: t.Function.Parameters,
			})
		case t.Name != nil:
			unified = append(unified, UnifiedTool{
				Name:        *t.Name,
				Description: derefString(t.Description),
				InputSchema: t.InputSchema,
			})
		default:
			slog.Warn("skipping invalid tool: no function or name field found")
		}
	}

	if len(unified) == 0 {
		return nil
	}
	return unified
}

// derefString safely dereferences a *string, returning "" if nil.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// truncateStr truncates a string to maxLen characters for logging.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
