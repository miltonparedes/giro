package stream

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// --- Helpers ---

// chanToSlice drains a KiroEvent channel into a slice.
func chanToSlice(ch <-chan KiroEvent) []KiroEvent {
	var out []KiroEvent
	for evt := range ch {
		out = append(out, evt)
	}
	return out
}

// fakeBody returns an io.ReadCloser that yields the given chunks with an
// optional delay between them.
func fakeBody(chunks ...string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(strings.Join(chunks, "")))
}

// slowBody returns an io.ReadCloser that blocks for delay before returning data.
type slowBody struct {
	delay time.Duration
	data  string
	done  bool
}

func (s *slowBody) Read(p []byte) (int, error) {
	if s.done {
		return 0, io.EOF
	}
	time.Sleep(s.delay)
	s.done = true
	n := copy(p, s.data)
	return n, io.EOF
}

func (s *slowBody) Close() error { return nil }

// emptyBody returns an io.ReadCloser that immediately returns EOF.
func emptyBody() io.ReadCloser {
	return io.NopCloser(strings.NewReader(""))
}

// --- Tests ---

func TestParseKiroStream_SimpleContent(t *testing.T) {
	// Build a body that contains a Kiro content event.
	body := fakeBody(`{"content":"Hello"}`, `{"content":" World"}`)

	cfg := Config{
		FirstTokenTimeout: 5 * time.Second,
	}

	events := chanToSlice(ParseKiroStream(context.Background(), body, cfg))

	var contentParts []string
	for _, evt := range events {
		if evt.Type == "content" {
			contentParts = append(contentParts, evt.Content)
		}
	}

	combined := strings.Join(contentParts, "")
	if combined != "Hello World" {
		t.Fatalf("expected content %q, got %q", "Hello World", combined)
	}
}

func TestParseKiroStream_EmptyResponse(t *testing.T) {
	body := emptyBody()

	cfg := Config{
		FirstTokenTimeout: 5 * time.Second,
	}

	events := chanToSlice(ParseKiroStream(context.Background(), body, cfg))

	if len(events) != 0 {
		t.Fatalf("expected 0 events for empty response, got %d", len(events))
	}
}

func TestParseKiroStream_FirstTokenTimeout(t *testing.T) {
	body := &slowBody{delay: 2 * time.Second, data: `{"content":"late"}`}

	cfg := Config{
		FirstTokenTimeout: 50 * time.Millisecond,
	}

	events := chanToSlice(ParseKiroStream(context.Background(), body, cfg))

	if len(events) != 1 {
		t.Fatalf("expected 1 error event, got %d events", len(events))
	}

	if events[0].Type != "error" {
		t.Fatalf("expected error event, got %q", events[0].Type)
	}

	var ftErr *FirstTokenTimeoutError
	if !errors.As(events[0].Error, &ftErr) {
		t.Fatalf("expected FirstTokenTimeoutError, got %T: %v", events[0].Error, events[0].Error)
	}
}

func TestParseKiroStream_UsageEvent(t *testing.T) {
	body := fakeBody(`{"usage":{"credits":0.001}}`)

	cfg := Config{
		FirstTokenTimeout: 5 * time.Second,
	}

	events := chanToSlice(ParseKiroStream(context.Background(), body, cfg))

	var usageEvents []KiroEvent
	for _, evt := range events {
		if evt.Type == "usage" {
			usageEvents = append(usageEvents, evt)
		}
	}

	if len(usageEvents) != 1 {
		t.Fatalf("expected 1 usage event, got %d", len(usageEvents))
	}

	credits, ok := usageEvents[0].Usage["credits"]
	if !ok {
		t.Fatal("expected credits in usage data")
	}

	if credits.(float64) != 0.001 {
		t.Fatalf("expected credits 0.001, got %v", credits)
	}
}

func TestParseKiroStream_ContextUsageEvent(t *testing.T) {
	body := fakeBody(`{"contextUsagePercentage":5.5}`)

	cfg := Config{
		FirstTokenTimeout: 5 * time.Second,
	}

	events := chanToSlice(ParseKiroStream(context.Background(), body, cfg))

	var contextEvents []KiroEvent
	for _, evt := range events {
		if evt.Type == "context_usage" {
			contextEvents = append(contextEvents, evt)
		}
	}

	if len(contextEvents) != 1 {
		t.Fatalf("expected 1 context_usage event, got %d", len(contextEvents))
	}

	if contextEvents[0].ContextUsagePercent != 5.5 {
		t.Fatalf("expected context usage 5.5, got %f", contextEvents[0].ContextUsagePercent)
	}
}

func TestParseKiroStream_ToolCalls(t *testing.T) {
	// Simulate a tool start+stop sequence.
	body := fakeBody(
		`{"content":"thinking"}`,
		`{"name":"get_weather","toolUseId":"call_abc","input":"{\"city\":\"NYC\"}","stop":true}`,
	)

	cfg := Config{
		FirstTokenTimeout: 5 * time.Second,
	}

	events := chanToSlice(ParseKiroStream(context.Background(), body, cfg))

	var toolEvents []KiroEvent
	for _, evt := range events {
		if evt.Type == "tool_use" {
			toolEvents = append(toolEvents, evt)
		}
	}

	if len(toolEvents) != 1 {
		t.Fatalf("expected 1 tool_use event, got %d", len(toolEvents))
	}

	tu := toolEvents[0].ToolUse
	if tu.Name != "get_weather" {
		t.Fatalf("expected tool name %q, got %q", "get_weather", tu.Name)
	}
	if tu.ID != "call_abc" {
		t.Fatalf("expected tool ID %q, got %q", "call_abc", tu.ID)
	}
}

func TestParseKiroStream_WithThinkingParser(t *testing.T) {
	body := fakeBody(`{"content":"<thinking>deep thought</thinking>response"}`)

	cfg := Config{
		FakeReasoning:         true,
		FakeReasoningHandling: HandlingAsReasoning,
		InitialBufferSize:     20,
		FirstTokenTimeout:     5 * time.Second,
	}

	events := chanToSlice(ParseKiroStream(context.Background(), body, cfg))

	var thinkingContent, regularContent string
	for _, evt := range events {
		switch evt.Type {
		case "thinking":
			thinkingContent += evt.ThinkingContent
		case "content":
			regularContent += evt.Content
		}
	}

	if thinkingContent != "deep thought" {
		t.Fatalf("expected thinking %q, got %q", "deep thought", thinkingContent)
	}
	if regularContent != "response" {
		t.Fatalf("expected regular %q, got %q", "response", regularContent)
	}
}

func TestParseKiroStream_ContextCancellation(_ *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	body := fakeBody(`{"content":"Hello"}`)

	cfg := Config{
		FirstTokenTimeout: 5 * time.Second,
	}

	// When context is canceled before reading, we should get an error event
	// (timeout) or no events at all. Either is acceptable.
	// Draining the channel verifies there is no deadlock.
	chanToSlice(ParseKiroStream(ctx, body, cfg))
}

func TestFirstTokenTimeoutError_Message(t *testing.T) {
	err := &FirstTokenTimeoutError{}
	if err.Error() != firstTokMsg {
		t.Fatalf("expected %q, got %q", firstTokMsg, err.Error())
	}
}
