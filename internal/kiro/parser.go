package kiro

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// ToolCallResult represents a parsed tool call extracted from the event stream.
type ToolCallResult struct {
	ID                 string
	Name               string
	Arguments          string // JSON string
	TruncationDetected bool
	TruncationInfo     *TruncationInfo
}

// TruncationInfo holds diagnostic information about JSON truncation.
type TruncationInfo struct {
	IsTruncated bool
	Reason      string
	SizeBytes   int
}

// ParserEvent represents a parsed event from the AWS event stream.
type ParserEvent struct {
	Type string // "content", "usage", "context_usage"
	Data any
}

// AwsEventStreamParser parses the binary AWS event stream format into structured events.
//
// AWS returns events in binary format with :message-type...event delimiters.
// This parser extracts JSON events from the stream and converts them to ParserEvent values.
//
// Supported event types: content, tool_start, tool_input, tool_stop, usage, context_usage.
type AwsEventStreamParser struct {
	buffer          string
	lastContent     *string
	currentToolCall *ToolCallResult
	toolCalls       []ToolCallResult
}

// eventPattern maps a JSON prefix to an event type for stream parsing.
type eventPattern struct {
	prefix    string
	eventType string
}

var eventPatterns = []eventPattern{
	{`{"content":`, "content"},
	{`{"name":`, "tool_start"},
	{`{"input":`, "tool_input"},
	{`{"stop":`, "tool_stop"},
	{`{"followupPrompt":`, "followup"},
	{`{"usage":`, "usage"},
	{`{"contextUsagePercentage":`, "context_usage"},
}

var bracketToolCallRe = regexp.MustCompile(`(?i)\[Called\s+(\w+)\s+with\s+args:\s*`)

// generateToolCallID returns a unique tool call identifier.
func generateToolCallID() string {
	return "call_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
}

// FindMatchingBrace returns the index of the closing '}' that matches the opening
// brace at startPos, correctly handling nesting, string literals, and escape sequences.
// It returns -1 if the JSON is incomplete or startPos does not point to '{'.
func FindMatchingBrace(text string, startPos int) int {
	if startPos >= len(text) || text[startPos] != '{' {
		return -1
	}

	braceCount := 0
	inString := false
	escapeNext := false

	for i := startPos; i < len(text); i++ {
		ch := text[i]

		if escapeNext {
			escapeNext = false
			continue
		}

		if ch == '\\' && inString {
			escapeNext = true
			continue
		}

		if ch == '"' {
			inString = !inString
			continue
		}

		if !inString {
			switch ch {
			case '{':
				braceCount++
			case '}':
				braceCount--
				if braceCount == 0 {
					return i
				}
			}
		}
	}

	return -1
}

// ParseBracketToolCalls extracts tool calls written in the bracket format
// "[Called func_name with args: {...}]" from response text.
func ParseBracketToolCalls(responseText string) []ToolCallResult {
	if !strings.Contains(responseText, "[Called") {
		return nil
	}

	var results []ToolCallResult

	for _, match := range bracketToolCallRe.FindAllStringSubmatchIndex(responseText, -1) {
		funcName := responseText[match[2]:match[3]]
		argsStart := match[1]

		jsonStart := strings.Index(responseText[argsStart:], "{")
		if jsonStart == -1 {
			continue
		}
		jsonStart += argsStart

		jsonEnd := FindMatchingBrace(responseText, jsonStart)
		if jsonEnd == -1 {
			continue
		}

		jsonStr := responseText[jsonStart : jsonEnd+1]

		var parsed any
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
			slog.Warn("failed to parse bracket tool call arguments", "func", funcName, "error", err)
			continue
		}

		normalized, _ := json.Marshal(parsed)

		results = append(results, ToolCallResult{
			ID:        generateToolCallID(),
			Name:      funcName,
			Arguments: string(normalized),
		})
	}

	return results
}

// DeduplicateToolCalls removes duplicate tool calls using a two-pass approach:
// first by ID (keeping the entry with longer/non-empty arguments), then by
// name+arguments (keeping the first occurrence).
func DeduplicateToolCalls(toolCalls []ToolCallResult) []ToolCallResult {
	if len(toolCalls) == 0 {
		return toolCalls
	}

	byID := make(map[string]*ToolCallResult)
	var orderIDs []string
	var noID []ToolCallResult

	for i := range toolCalls {
		tc := &toolCalls[i]
		if tc.ID == "" {
			noID = append(noID, *tc)
			continue
		}

		existing, ok := byID[tc.ID]
		if !ok {
			byID[tc.ID] = tc
			orderIDs = append(orderIDs, tc.ID)
			continue
		}

		if tc.Arguments != "{}" && (existing.Arguments == "{}" || len(tc.Arguments) > len(existing.Arguments)) {
			slog.Debug("replacing tool call with better arguments",
				"id", tc.ID,
				"old_len", len(existing.Arguments),
				"new_len", len(tc.Arguments),
			)
			byID[tc.ID] = tc
		}
	}

	// Collect: with-ID in insertion order, then no-ID.
	merged := make([]ToolCallResult, 0, len(orderIDs)+len(noID))
	for _, id := range orderIDs {
		if tc, ok := byID[id]; ok {
			merged = append(merged, *tc)
		}
	}
	merged = append(merged, noID...)

	return deduplicateByNameArgs(merged, len(toolCalls))
}

// deduplicateByNameArgs removes entries that share the same name+arguments key.
func deduplicateByNameArgs(toolCalls []ToolCallResult, originalLen int) []ToolCallResult {
	seen := make(map[string]struct{})
	unique := make([]ToolCallResult, 0, len(toolCalls))

	for _, tc := range toolCalls {
		key := tc.Name + "-" + tc.Arguments
		if _, dup := seen[key]; !dup {
			seen[key] = struct{}{}
			unique = append(unique, tc)
		}
	}

	if originalLen != len(unique) {
		slog.Debug("deduplicated tool calls", "before", originalLen, "after", len(unique))
	}

	return unique
}

// DiagnoseJSONTruncation analyses a malformed JSON string to determine whether it
// was truncated (e.g. by the upstream Kiro API) or is simply invalid.
func DiagnoseJSONTruncation(jsonStr string) TruncationInfo {
	sizeBytes := len([]byte(jsonStr))
	stripped := strings.TrimSpace(jsonStr)

	if stripped == "" {
		return TruncationInfo{IsTruncated: false, Reason: "empty string", SizeBytes: sizeBytes}
	}

	if info, ok := checkMissingClosers(stripped, sizeBytes); ok {
		return info
	}

	if info, ok := checkUnbalanced(stripped, sizeBytes); ok {
		return info
	}

	if unclosedString(stripped) {
		return TruncationInfo{IsTruncated: true, Reason: "unclosed string literal", SizeBytes: sizeBytes}
	}

	return TruncationInfo{IsTruncated: false, Reason: "malformed JSON", SizeBytes: sizeBytes}
}

// checkMissingClosers detects JSON that starts with { or [ but does not end
// with the matching closer.
func checkMissingClosers(s string, size int) (TruncationInfo, bool) {
	if s[0] == '{' && s[len(s)-1] != '}' {
		missing := strings.Count(s, "{") - strings.Count(s, "}")
		return TruncationInfo{
			IsTruncated: true,
			Reason:      fmt.Sprintf("missing %d closing brace(s)", missing),
			SizeBytes:   size,
		}, true
	}

	if s[0] == '[' && s[len(s)-1] != ']' {
		missing := strings.Count(s, "[") - strings.Count(s, "]")
		return TruncationInfo{
			IsTruncated: true,
			Reason:      fmt.Sprintf("missing %d closing bracket(s)", missing),
			SizeBytes:   size,
		}, true
	}

	return TruncationInfo{}, false
}

// checkUnbalanced detects unbalanced braces or brackets.
func checkUnbalanced(s string, size int) (TruncationInfo, bool) {
	openBraces := strings.Count(s, "{")
	closeBraces := strings.Count(s, "}")

	if openBraces != closeBraces {
		return TruncationInfo{
			IsTruncated: true,
			Reason:      fmt.Sprintf("unbalanced braces (%d open, %d close)", openBraces, closeBraces),
			SizeBytes:   size,
		}, true
	}

	openBrackets := strings.Count(s, "[")
	closeBrackets := strings.Count(s, "]")

	if openBrackets != closeBrackets {
		return TruncationInfo{
			IsTruncated: true,
			Reason:      fmt.Sprintf("unbalanced brackets (%d open, %d close)", openBrackets, closeBrackets),
			SizeBytes:   size,
		}, true
	}

	return TruncationInfo{}, false
}

// unclosedString returns true when the number of unescaped double-quotes is odd.
func unclosedString(s string) bool {
	count := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++ // skip escaped char
			continue
		}
		if s[i] == '"' {
			count++
		}
	}
	return count%2 != 0
}

// NewAwsEventStreamParser creates a ready-to-use AwsEventStreamParser.
func NewAwsEventStreamParser() *AwsEventStreamParser {
	return &AwsEventStreamParser{}
}

// Feed decodes chunk as UTF-8 (ignoring errors), appends it to the internal
// buffer, and returns all complete events found so far.
func (p *AwsEventStreamParser) Feed(chunk []byte) []ParserEvent {
	p.buffer += string(chunk)

	var events []ParserEvent

	for {
		pos, evtType := p.findEarliestPattern()
		if pos == -1 {
			break
		}

		jsonEnd := FindMatchingBrace(p.buffer, pos)
		if jsonEnd == -1 {
			break // incomplete JSON, wait for more data
		}

		jsonStr := p.buffer[pos : jsonEnd+1]
		p.buffer = p.buffer[jsonEnd+1:]

		var data map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
			slog.Warn("failed to parse event JSON", "error", err)
			continue
		}

		if evt := p.processEvent(data, evtType); evt != nil {
			events = append(events, *evt)
		}
	}

	return events
}

// findEarliestPattern scans the buffer for the first occurrence of any known
// JSON event prefix and returns its position and type.
func (p *AwsEventStreamParser) findEarliestPattern() (int, string) {
	earliestPos := -1
	earliestType := ""

	for _, ep := range eventPatterns {
		pos := strings.Index(p.buffer, ep.prefix)
		if pos != -1 && (earliestPos == -1 || pos < earliestPos) {
			earliestPos = pos
			earliestType = ep.eventType
		}
	}

	return earliestPos, earliestType
}

// processEvent dispatches a parsed JSON object to the correct handler.
func (p *AwsEventStreamParser) processEvent(data map[string]any, eventType string) *ParserEvent {
	switch eventType {
	case "content":
		return p.processContentEvent(data)
	case "tool_start":
		return p.processToolStartEvent(data)
	case "tool_input":
		return p.processToolInputEvent(data)
	case "tool_stop":
		return p.processToolStopEvent(data)
	case "usage":
		return &ParserEvent{Type: "usage", Data: data["usage"]}
	case "context_usage":
		return &ParserEvent{Type: "context_usage", Data: data["contextUsagePercentage"]}
	default:
		return nil
	}
}

// processContentEvent handles a content event, deduplicating repeated values
// and skipping events that contain a followupPrompt.
func (p *AwsEventStreamParser) processContentEvent(data map[string]any) *ParserEvent {
	if _, ok := data["followupPrompt"]; ok {
		return nil
	}

	content, _ := data["content"].(string)

	if p.lastContent != nil && *p.lastContent == content {
		return nil
	}

	p.lastContent = &content

	return &ParserEvent{Type: "content", Data: content}
}

// processToolStartEvent handles a tool_start event: finalizes any in-progress
// tool call, then creates a new one from the data.
func (p *AwsEventStreamParser) processToolStartEvent(data map[string]any) *ParserEvent {
	if p.currentToolCall != nil {
		p.finalizeToolCall()
	}

	id, _ := data["toolUseId"].(string)
	if id == "" {
		id = generateToolCallID()
	}

	name, _ := data["name"].(string)
	inputStr := extractInput(data)

	p.currentToolCall = &ToolCallResult{
		ID:        id,
		Name:      name,
		Arguments: inputStr,
	}

	if _, ok := data["stop"]; ok {
		p.finalizeToolCall()
	}

	return nil
}

// extractInput converts the "input" field to a string, handling both string
// and map values.
func extractInput(data map[string]any) string {
	raw, ok := data["input"]
	if !ok || raw == nil {
		return ""
	}

	switch v := raw.(type) {
	case string:
		return v
	case map[string]any:
		b, _ := json.Marshal(v)
		return string(b)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// processToolInputEvent appends input data to the current in-progress tool call.
func (p *AwsEventStreamParser) processToolInputEvent(data map[string]any) *ParserEvent {
	if p.currentToolCall != nil {
		p.currentToolCall.Arguments += extractInput(data)
	}
	return nil
}

// processToolStopEvent finalizes the current tool call when a stop event arrives.
func (p *AwsEventStreamParser) processToolStopEvent(data map[string]any) *ParserEvent {
	if p.currentToolCall != nil {
		if _, ok := data["stop"]; ok {
			p.finalizeToolCall()
		}
	}
	return nil
}

// finalizeToolCall normalizes the current tool call's arguments as JSON, detects
// truncation if parsing fails, and appends the result to the completed list.
func (p *AwsEventStreamParser) finalizeToolCall() {
	if p.currentToolCall == nil {
		return
	}

	args := strings.TrimSpace(p.currentToolCall.Arguments)

	if args == "" {
		slog.Debug("tool has empty arguments string", "tool", p.currentToolCall.Name)
		p.currentToolCall.Arguments = "{}"
		p.toolCalls = append(p.toolCalls, *p.currentToolCall)
		p.currentToolCall = nil
		return
	}

	p.normalizeArguments(args)
	p.toolCalls = append(p.toolCalls, *p.currentToolCall)
	p.currentToolCall = nil
}

// normalizeArguments attempts to parse and re-serialize the arguments JSON. On
// failure it diagnoses truncation and falls back to "{}".
func (p *AwsEventStreamParser) normalizeArguments(args string) {
	var parsed any
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		info := DiagnoseJSONTruncation(args)
		if info.IsTruncated {
			p.currentToolCall.TruncationDetected = true
			p.currentToolCall.TruncationInfo = &info
			slog.Error("tool call truncated by Kiro API",
				"tool", p.currentToolCall.Name,
				"id", p.currentToolCall.ID,
				"size_bytes", info.SizeBytes,
				"reason", info.Reason,
			)
		} else {
			slog.Warn("failed to parse tool arguments",
				"tool", p.currentToolCall.Name,
				"error", err,
			)
		}
		p.currentToolCall.Arguments = "{}"
		return
	}

	normalized, _ := json.Marshal(parsed)
	p.currentToolCall.Arguments = string(normalized)
	slog.Debug("tool arguments parsed successfully", "tool", p.currentToolCall.Name)
}

// GetToolCalls finalizes any in-progress tool call and returns the deduplicated
// list of all collected tool calls.
func (p *AwsEventStreamParser) GetToolCalls() []ToolCallResult {
	if p.currentToolCall != nil {
		p.finalizeToolCall()
	}
	return DeduplicateToolCalls(p.toolCalls)
}

// Reset clears all parser state so the instance can be reused.
func (p *AwsEventStreamParser) Reset() {
	p.buffer = ""
	p.lastContent = nil
	p.currentToolCall = nil
	p.toolCalls = nil
}
