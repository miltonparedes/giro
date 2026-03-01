package kiro_test

import (
	"strings"
	"testing"

	"github.com/miltonparedes/giro/internal/kiro"
)

// ---------------------------------------------------------------------------
// FindMatchingBrace
// ---------------------------------------------------------------------------

func TestFindMatchingBrace_Simple(t *testing.T) {
	text := `{"key": "value"}`
	got := kiro.FindMatchingBrace(text, 0)
	if got != 15 {
		t.Errorf("FindMatchingBrace simple = %d, want 15", got)
	}
}

func TestFindMatchingBrace_Nested(t *testing.T) {
	text := `{"outer": {"inner": "value"}}`
	got := kiro.FindMatchingBrace(text, 0)
	if got != 28 {
		t.Errorf("FindMatchingBrace nested = %d, want 28", got)
	}
}

func TestFindMatchingBrace_BracesInString(t *testing.T) {
	text := `{"text": "Hello {world}"}`
	got := kiro.FindMatchingBrace(text, 0)
	if got != 24 {
		t.Errorf("FindMatchingBrace braces in string = %d, want 24", got)
	}
}

func TestFindMatchingBrace_EscapedQuotes(t *testing.T) {
	text := `{"text": "Say \"hello\""}`
	got := kiro.FindMatchingBrace(text, 0)
	if got != 24 {
		t.Errorf("FindMatchingBrace escaped quotes = %d, want 24", got)
	}
}

func TestFindMatchingBrace_Incomplete(t *testing.T) {
	text := `{"key": "value"`
	got := kiro.FindMatchingBrace(text, 0)
	if got != -1 {
		t.Errorf("FindMatchingBrace incomplete = %d, want -1", got)
	}
}

func TestFindMatchingBrace_NotStartingWithBrace(t *testing.T) {
	text := `hello {"key": "value"}`
	got := kiro.FindMatchingBrace(text, 0)
	if got != -1 {
		t.Errorf("FindMatchingBrace not starting with brace = %d, want -1", got)
	}
}

func TestFindMatchingBrace_OutOfBounds(t *testing.T) {
	text := `{"a":1}`
	got := kiro.FindMatchingBrace(text, 100)
	if got != -1 {
		t.Errorf("FindMatchingBrace out of bounds = %d, want -1", got)
	}
}

// ---------------------------------------------------------------------------
// ParseBracketToolCalls
// ---------------------------------------------------------------------------

func TestParseBracketToolCalls_SingleCall(t *testing.T) {
	text := `[Called get_weather with args: {"location": "Moscow"}]`
	results := kiro.ParseBracketToolCalls(text)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "get_weather" {
		t.Errorf("name = %q, want %q", results[0].Name, "get_weather")
	}
	if !strings.Contains(results[0].Arguments, "location") {
		t.Errorf("arguments %q should contain 'location'", results[0].Arguments)
	}
}

func TestParseBracketToolCalls_MultipleCalls(t *testing.T) {
	text := `[Called get_weather with args: {"location": "Moscow"}]
Some text
[Called get_time with args: {"timezone": "UTC"}]`

	results := kiro.ParseBracketToolCalls(text)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Name != "get_weather" {
		t.Errorf("first name = %q, want %q", results[0].Name, "get_weather")
	}
	if results[1].Name != "get_time" {
		t.Errorf("second name = %q, want %q", results[1].Name, "get_time")
	}
}

func TestParseBracketToolCalls_NoCalls(t *testing.T) {
	text := "This is just regular text without any tool calls."
	results := kiro.ParseBracketToolCalls(text)
	if results != nil {
		t.Errorf("expected nil, got %v", results)
	}
}

func TestParseBracketToolCalls_EmptyString(t *testing.T) {
	results := kiro.ParseBracketToolCalls("")
	if results != nil {
		t.Errorf("expected nil, got %v", results)
	}
}

func TestParseBracketToolCalls_InvalidJSON(t *testing.T) {
	text := `[Called bad_func with args: {not valid json}]`
	results := kiro.ParseBracketToolCalls(text)
	if len(results) != 0 {
		t.Errorf("expected 0 results for invalid JSON, got %d", len(results))
	}
}

func TestParseBracketToolCalls_NestedJSON(t *testing.T) {
	text := `[Called complex_func with args: {"data": {"nested": {"deep": "value"}}}]`
	results := kiro.ParseBracketToolCalls(text)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !strings.Contains(results[0].Arguments, "nested") {
		t.Errorf("arguments %q should contain 'nested'", results[0].Arguments)
	}
}

func TestParseBracketToolCalls_UniqueIDs(t *testing.T) {
	text := `[Called func with args: {"a": 1}]
[Called func with args: {"a": 1}]`

	results := kiro.ParseBracketToolCalls(text)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID == results[1].ID {
		t.Errorf("expected unique IDs, both got %q", results[0].ID)
	}
}

// ---------------------------------------------------------------------------
// DeduplicateToolCalls
// ---------------------------------------------------------------------------

func TestDeduplicateToolCalls_ByNameArgs(t *testing.T) {
	calls := []kiro.ToolCallResult{
		{ID: "1", Name: "func", Arguments: `{"a":1}`},
		{ID: "2", Name: "func", Arguments: `{"a":1}`},
		{ID: "3", Name: "other", Arguments: `{"b":2}`},
	}
	result := kiro.DeduplicateToolCalls(calls)
	if len(result) != 2 {
		t.Errorf("expected 2 unique, got %d", len(result))
	}
}

func TestDeduplicateToolCalls_ByID_KeepLongerArgs(t *testing.T) {
	calls := []kiro.ToolCallResult{
		{ID: "call_123", Name: "func", Arguments: "{}"},
		{ID: "call_123", Name: "func", Arguments: `{"location":"Moscow"}`},
	}
	result := kiro.DeduplicateToolCalls(calls)
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if !strings.Contains(result[0].Arguments, "Moscow") {
		t.Errorf("should keep non-empty arguments, got %q", result[0].Arguments)
	}
}

func TestDeduplicateToolCalls_ByID_PreferLonger(t *testing.T) {
	calls := []kiro.ToolCallResult{
		{ID: "call_abc", Name: "search", Arguments: `{"q":"test"}`},
		{ID: "call_abc", Name: "search", Arguments: `{"q":"test","limit":10,"offset":0}`},
	}
	result := kiro.DeduplicateToolCalls(calls)
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if !strings.Contains(result[0].Arguments, "limit") {
		t.Errorf("should keep longer arguments, got %q", result[0].Arguments)
	}
}

func TestDeduplicateToolCalls_PreservesFirst(t *testing.T) {
	calls := []kiro.ToolCallResult{
		{ID: "first", Name: "func", Arguments: `{"a":1}`},
		{ID: "second", Name: "func", Arguments: `{"a":1}`},
	}
	result := kiro.DeduplicateToolCalls(calls)
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if result[0].ID != "first" {
		t.Errorf("should keep first occurrence, got ID %q", result[0].ID)
	}
}

func TestDeduplicateToolCalls_Empty(t *testing.T) {
	result := kiro.DeduplicateToolCalls(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
}

func TestDeduplicateToolCalls_NoDuplicates(t *testing.T) {
	calls := []kiro.ToolCallResult{
		{ID: "1", Name: "func1", Arguments: `{"a":1}`},
		{ID: "2", Name: "func2", Arguments: `{"b":2}`},
	}
	result := kiro.DeduplicateToolCalls(calls)
	if len(result) != 2 {
		t.Errorf("expected 2, got %d", len(result))
	}
}

func TestDeduplicateToolCalls_WithoutID(t *testing.T) {
	calls := []kiro.ToolCallResult{
		{ID: "", Name: "func", Arguments: `{"a":1}`},
		{ID: "", Name: "func", Arguments: `{"a":1}`},
		{ID: "", Name: "func", Arguments: `{"b":2}`},
	}
	result := kiro.DeduplicateToolCalls(calls)
	if len(result) != 2 {
		t.Errorf("expected 2 unique, got %d", len(result))
	}
}

func TestDeduplicateToolCalls_MixedIDAndNoID(t *testing.T) {
	calls := []kiro.ToolCallResult{
		{ID: "call_1", Name: "func1", Arguments: `{"x":1}`},
		{ID: "call_1", Name: "func1", Arguments: "{}"},
		{ID: "", Name: "func2", Arguments: `{"y":2}`},
		{ID: "", Name: "func2", Arguments: `{"y":2}`},
	}
	result := kiro.DeduplicateToolCalls(calls)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}

	var call1 *kiro.ToolCallResult
	for i := range result {
		if result[i].ID == "call_1" {
			call1 = &result[i]
			break
		}
	}
	if call1 == nil {
		t.Fatal("call_1 not found in result")
	}
	if call1.Arguments != `{"x":1}` {
		t.Errorf("call_1 should keep non-empty args, got %q", call1.Arguments)
	}
}

// ---------------------------------------------------------------------------
// DiagnoseJSONTruncation
// ---------------------------------------------------------------------------

func TestDiagnoseJSONTruncation_Empty(t *testing.T) {
	info := kiro.DiagnoseJSONTruncation("")
	if info.IsTruncated {
		t.Error("empty string should not be truncated")
	}
	if info.Reason != "empty string" {
		t.Errorf("reason = %q, want %q", info.Reason, "empty string")
	}
	if info.SizeBytes != 0 {
		t.Errorf("size = %d, want 0", info.SizeBytes)
	}
}

func TestDiagnoseJSONTruncation_ValidLooking(t *testing.T) {
	info := kiro.DiagnoseJSONTruncation(`{"key": "value", "number": 42}`)
	if info.IsTruncated {
		t.Error("valid-looking JSON should not be truncated")
	}
	if info.Reason != "malformed JSON" {
		t.Errorf("reason = %q, want %q", info.Reason, "malformed JSON")
	}
}

func TestDiagnoseJSONTruncation_MissingClosingBrace(t *testing.T) {
	info := kiro.DiagnoseJSONTruncation(`{"filePath": "/path/to/file.md"`)
	if !info.IsTruncated {
		t.Error("missing closing brace should be truncated")
	}
	if !strings.Contains(info.Reason, "brace") {
		t.Errorf("reason %q should mention brace", info.Reason)
	}
}

func TestDiagnoseJSONTruncation_MultipleNestedMissing(t *testing.T) {
	info := kiro.DiagnoseJSONTruncation(`{"outer": {"inner": {"deep": "value"`)
	if !info.IsTruncated {
		t.Error("multiple missing braces should be truncated")
	}
	if !strings.Contains(info.Reason, "3") || !strings.Contains(info.Reason, "brace") {
		t.Errorf("reason %q should mention 3 missing braces", info.Reason)
	}
}

func TestDiagnoseJSONTruncation_MissingClosingBracket(t *testing.T) {
	info := kiro.DiagnoseJSONTruncation(`[1, 2, 3, {"key": "value"}`)
	if !info.IsTruncated {
		t.Error("missing closing bracket should be truncated")
	}
	if !strings.Contains(info.Reason, "bracket") {
		t.Errorf("reason %q should mention bracket", info.Reason)
	}
}

func TestDiagnoseJSONTruncation_UnbalancedBraces(t *testing.T) {
	full := `{"a": {"b": 1}}`
	info := kiro.DiagnoseJSONTruncation(full[:len(full)-1])
	if !info.IsTruncated {
		t.Error("unbalanced braces should be truncated")
	}
}

func TestDiagnoseJSONTruncation_UnbalancedBrackets(t *testing.T) {
	info := kiro.DiagnoseJSONTruncation(`{"items": [[1, 2], [3, 4]}`)
	if !info.IsTruncated {
		t.Error("unbalanced brackets should be truncated")
	}
	if !strings.Contains(info.Reason, "bracket") {
		t.Errorf("reason %q should mention bracket", info.Reason)
	}
}

func TestDiagnoseJSONTruncation_UnclosedString(t *testing.T) {
	info := kiro.DiagnoseJSONTruncation(`{"content": "This is a very long string that was cut off`)
	if !info.IsTruncated {
		t.Error("unclosed string should be truncated")
	}
}

func TestDiagnoseJSONTruncation_EscapedQuotesValid(t *testing.T) {
	info := kiro.DiagnoseJSONTruncation(`{"text": "Say \"hello\" to everyone"}`)
	if info.IsTruncated {
		t.Error("properly escaped quotes should not be truncated")
	}
}

func TestDiagnoseJSONTruncation_MalformedNotTruncated(t *testing.T) {
	info := kiro.DiagnoseJSONTruncation(`{"key": "value",}`)
	if info.IsTruncated {
		t.Error("trailing comma is malformed, not truncated")
	}
	if info.Reason != "malformed JSON" {
		t.Errorf("reason = %q, want %q", info.Reason, "malformed JSON")
	}
}

func TestDiagnoseJSONTruncation_OnlyOpenBrace(t *testing.T) {
	info := kiro.DiagnoseJSONTruncation("{")
	if !info.IsTruncated {
		t.Error("single opening brace should be truncated")
	}
	if !strings.Contains(info.Reason, "brace") {
		t.Errorf("reason %q should mention brace", info.Reason)
	}
}

func TestDiagnoseJSONTruncation_OnlyOpenBracket(t *testing.T) {
	info := kiro.DiagnoseJSONTruncation("[")
	if !info.IsTruncated {
		t.Error("single opening bracket should be truncated")
	}
	if !strings.Contains(info.Reason, "bracket") {
		t.Errorf("reason %q should mention bracket", info.Reason)
	}
}

func TestDiagnoseJSONTruncation_SizeBytesUTF8(t *testing.T) {
	jsonStr := `{"city": "Москва"`
	info := kiro.DiagnoseJSONTruncation(jsonStr)
	expectedSize := len([]byte(jsonStr))
	if info.SizeBytes != expectedSize {
		t.Errorf("size = %d, want %d", info.SizeBytes, expectedSize)
	}
	if !info.IsTruncated {
		t.Error("missing closing brace should be truncated")
	}
}

// ---------------------------------------------------------------------------
// AwsEventStreamParser.Feed -- content events
// ---------------------------------------------------------------------------

func TestFeed_ContentEvent(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	events := p.Feed([]byte(`{"content":"Hello World"}`))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "content" {
		t.Errorf("type = %q, want %q", events[0].Type, "content")
	}
	if events[0].Data != "Hello World" {
		t.Errorf("data = %v, want %q", events[0].Data, "Hello World")
	}
}

func TestFeed_MultipleContentEvents(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	events := p.Feed([]byte(`{"content":"First"}{"content":"Second"}`))
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Data != "First" {
		t.Errorf("first = %v, want %q", events[0].Data, "First")
	}
	if events[1].Data != "Second" {
		t.Errorf("second = %v, want %q", events[1].Data, "Second")
	}
}

func TestFeed_ContentDeduplication(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	events1 := p.Feed([]byte(`{"content":"Same"}`))
	events2 := p.Feed([]byte(`{"content":"Same"}`))
	if len(events1) != 1 {
		t.Errorf("first feed: expected 1 event, got %d", len(events1))
	}
	if len(events2) != 0 {
		t.Errorf("second feed: expected 0 events (dup), got %d", len(events2))
	}
}

func TestFeed_FollowupPromptSkipped(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	events := p.Feed([]byte(`{"content":"text","followupPrompt":"suggestion"}`))
	if len(events) != 0 {
		t.Errorf("expected 0 events (followupPrompt), got %d", len(events))
	}
}

// ---------------------------------------------------------------------------
// AwsEventStreamParser.Feed -- tool call lifecycle
// ---------------------------------------------------------------------------

func TestFeed_ToolCallLifecycle(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()

	events := p.Feed([]byte(`{"name":"get_weather","toolUseId":"call_123"}`))
	if len(events) != 0 {
		t.Errorf("tool_start should produce 0 events, got %d", len(events))
	}

	events = p.Feed([]byte(`{"input":"{\"city\": \"London\"}"}`))
	if len(events) != 0 {
		t.Errorf("tool_input should produce 0 events, got %d", len(events))
	}

	events = p.Feed([]byte(`{"stop":true}`))
	if len(events) != 0 {
		t.Errorf("tool_stop should produce 0 events, got %d", len(events))
	}

	calls := p.GetToolCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "get_weather" {
		t.Errorf("name = %q, want %q", calls[0].Name, "get_weather")
	}
	if calls[0].ID != "call_123" {
		t.Errorf("id = %q, want %q", calls[0].ID, "call_123")
	}
	if !strings.Contains(calls[0].Arguments, "London") {
		t.Errorf("arguments %q should contain 'London'", calls[0].Arguments)
	}
}

func TestFeed_ToolStartWithStop(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	p.Feed([]byte(`{"name":"quick_func","toolUseId":"call_fast","stop":true}`))

	calls := p.GetToolCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "quick_func" {
		t.Errorf("name = %q, want %q", calls[0].Name, "quick_func")
	}
}

func TestFeed_GetToolCallsFinalizesIncomplete(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	p.Feed([]byte(`{"name":"func","toolUseId":"call_1"}`))

	calls := p.GetToolCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call (finalized), got %d", len(calls))
	}
}

func TestFeed_MultipleToolCalls(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	p.Feed([]byte(`{"name":"func1","toolUseId":"call_1"}`))
	p.Feed([]byte(`{"stop":true}`))
	p.Feed([]byte(`{"name":"func2","toolUseId":"call_2"}`))
	p.Feed([]byte(`{"stop":true}`))

	calls := p.GetToolCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(calls))
	}
}

func TestFeed_ToolInputMapValue(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	p.Feed([]byte(`{"name":"func","toolUseId":"call_map"}`))
	p.Feed([]byte(`{"input":{"key":"value"}}`))
	p.Feed([]byte(`{"stop":true}`))

	calls := p.GetToolCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if !strings.Contains(calls[0].Arguments, "key") {
		t.Errorf("arguments %q should contain 'key'", calls[0].Arguments)
	}
}

func TestFeed_NewToolStartFinalizesPrevious(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	p.Feed([]byte(`{"name":"func_a","toolUseId":"id_a","input":"{\"a\":1}"}`))
	p.Feed([]byte(`{"name":"func_b","toolUseId":"id_b","input":"{\"b\":2}","stop":true}`))

	calls := p.GetToolCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(calls))
	}
	if calls[0].Name != "func_a" {
		t.Errorf("first call should be func_a, got %q", calls[0].Name)
	}
	if calls[1].Name != "func_b" {
		t.Errorf("second call should be func_b, got %q", calls[1].Name)
	}
}

// ---------------------------------------------------------------------------
// AwsEventStreamParser.Feed -- usage and context_usage
// ---------------------------------------------------------------------------

func TestFeed_UsageEvent(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	events := p.Feed([]byte(`{"usage":1.5}`))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "usage" {
		t.Errorf("type = %q, want %q", events[0].Type, "usage")
	}
	if val, ok := events[0].Data.(float64); !ok || val != 1.5 {
		t.Errorf("data = %v, want 1.5", events[0].Data)
	}
}

func TestFeed_ContextUsageEvent(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	events := p.Feed([]byte(`{"contextUsagePercentage":25.5}`))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "context_usage" {
		t.Errorf("type = %q, want %q", events[0].Type, "context_usage")
	}
	if val, ok := events[0].Data.(float64); !ok || val != 25.5 {
		t.Errorf("data = %v, want 25.5", events[0].Data)
	}
}

// ---------------------------------------------------------------------------
// AwsEventStreamParser.Feed -- buffering and edge cases
// ---------------------------------------------------------------------------

func TestFeed_IncompleteJSON(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	events := p.Feed([]byte(`{"content":"Hel`))
	if len(events) != 0 {
		t.Errorf("expected 0 events for incomplete JSON, got %d", len(events))
	}
}

func TestFeed_JSONAcrossChunks(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	events1 := p.Feed([]byte(`{"content":"Hel`))
	events2 := p.Feed([]byte(`lo World"}`))
	if len(events1) != 0 {
		t.Errorf("first chunk: expected 0, got %d", len(events1))
	}
	if len(events2) != 1 {
		t.Fatalf("second chunk: expected 1, got %d", len(events2))
	}
	if events2[0].Data != "Hello World" {
		t.Errorf("data = %v, want %q", events2[0].Data, "Hello World")
	}
}

func TestFeed_MixedEvents(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	chunk := `{"content":"Hello"}{"usage":1.0}{"contextUsagePercentage":50}`
	events := p.Feed([]byte(chunk))
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].Type != "content" {
		t.Errorf("event 0 type = %q, want %q", events[0].Type, "content")
	}
	if events[1].Type != "usage" {
		t.Errorf("event 1 type = %q, want %q", events[1].Type, "usage")
	}
	if events[2].Type != "context_usage" {
		t.Errorf("event 2 type = %q, want %q", events[2].Type, "context_usage")
	}
}

func TestFeed_GarbageBetweenEvents(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	events := p.Feed([]byte(`garbage{"content":"valid"}more garbage{"usage":1}`))
	if len(events) != 2 {
		t.Errorf("expected 2 events among garbage, got %d", len(events))
	}
}

func TestFeed_EmptyChunk(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	events := p.Feed([]byte{})
	if len(events) != 0 {
		t.Errorf("expected 0 events for empty chunk, got %d", len(events))
	}
}

func TestFeed_InvalidBytesRecovery(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	events := p.Feed([]byte{0xff, 0xfe})
	if len(events) != 0 {
		t.Errorf("invalid bytes alone should produce 0 events, got %d", len(events))
	}
	events = p.Feed([]byte(`{"content":"test"}`))
	if len(events) != 1 {
		t.Errorf("should recover and parse, got %d events", len(events))
	}
}

func TestFeed_EscapeSequences(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	events := p.Feed([]byte(`{"content":"Line1\nLine2"}`))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if data, ok := events[0].Data.(string); !ok || !strings.Contains(data, "\n") {
		t.Errorf("expected newline in content, got %v", events[0].Data)
	}
}

// ---------------------------------------------------------------------------
// AwsEventStreamParser.Reset
// ---------------------------------------------------------------------------

func TestReset(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	p.Feed([]byte(`{"content":"test"}`))
	p.Feed([]byte(`{"name":"func","toolUseId":"call_1"}`))

	p.Reset()

	events := p.Feed([]byte(`{"content":"test"}`))
	if len(events) != 1 {
		t.Errorf("after reset, content should not be deduplicated, got %d events", len(events))
	}

	calls := p.GetToolCalls()
	if len(calls) != 0 {
		t.Errorf("after reset, tool calls should be empty, got %d", len(calls))
	}
}

// ---------------------------------------------------------------------------
// AwsEventStreamParser -- finalize with truncation
// ---------------------------------------------------------------------------

func TestFeed_TruncatedToolCallMarked(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	p.Feed([]byte(`{"name":"write_to_file","toolUseId":"call_trunc"}`))
	p.Feed([]byte(`{"input":"{\"filePath\": \"/path/to/file.md\", \"content\": \"cut off"}`))
	p.Feed([]byte(`{"stop":true}`))

	calls := p.GetToolCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if !calls[0].TruncationDetected {
		t.Error("expected TruncationDetected to be true")
	}
	if calls[0].TruncationInfo == nil {
		t.Fatal("expected TruncationInfo to be non-nil")
	}
	if !calls[0].TruncationInfo.IsTruncated {
		t.Error("expected TruncationInfo.IsTruncated to be true")
	}
	if calls[0].TruncationInfo.SizeBytes <= 0 {
		t.Errorf("expected positive SizeBytes, got %d", calls[0].TruncationInfo.SizeBytes)
	}
	if calls[0].Arguments != "{}" {
		t.Errorf("truncated args should be '{}', got %q", calls[0].Arguments)
	}
}

func TestFeed_ValidToolCallNotMarkedTruncated(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	p.Feed([]byte(`{"name":"get_weather","toolUseId":"call_ok"}`))
	p.Feed([]byte(`{"input":"{\"location\": \"Moscow\"}"}`))
	p.Feed([]byte(`{"stop":true}`))

	calls := p.GetToolCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].TruncationDetected {
		t.Error("valid tool call should not be marked truncated")
	}
	if calls[0].TruncationInfo != nil {
		t.Error("valid tool call should have nil TruncationInfo")
	}
}

func TestFeed_EmptyArgsBecome(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	p.Feed([]byte(`{"name":"func","toolUseId":"call_empty"}`))
	p.Feed([]byte(`{"stop":true}`))

	calls := p.GetToolCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Arguments != "{}" {
		t.Errorf("empty args should become '{}', got %q", calls[0].Arguments)
	}
}

func TestFeed_GeneratedIDHasPrefix(t *testing.T) {
	p := kiro.NewAwsEventStreamParser()
	p.Feed([]byte(`{"name":"my_func","input":"{}","stop":true}`))

	calls := p.GetToolCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if !strings.HasPrefix(calls[0].ID, "call_") {
		t.Errorf("expected generated ID with call_ prefix, got %q", calls[0].ID)
	}
}
