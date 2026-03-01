package stream

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/miltonparedes/giro/internal/kiro"
)

// ToolUseEvent represents a tool invocation extracted from the stream.
type ToolUseEvent struct {
	ID                 string
	Name               string
	Arguments          string
	TruncationDetected bool
}

// KiroEvent is a unified event from the Kiro API stream. It is API-agnostic
// and can be converted to both OpenAI and Anthropic formats.
type KiroEvent struct {
	Type                 string         // content, thinking, tool_use, usage, context_usage, error
	Content              string         // text content (for content events)
	ThinkingContent      string         // reasoning content (for thinking events)
	ToolUse              *ToolUseEvent  // tool invocation (for tool_use events)
	Usage                map[string]any // metering data (for usage events)
	ContextUsagePercent  float64        // context usage percentage (for context_usage events)
	Error                error          // error (for error events)
	IsFirstThinkingChunk bool           // first thinking chunk in the stream
	IsLastThinkingChunk  bool           // last thinking chunk (closing tag found)
}

// Event type constants used by KiroEvent.Type and throughout the formatters.
const (
	EventContent      = "content"
	EventThinking     = "thinking"
	EventToolUse      = "tool_use"
	EventUsage        = "usage"
	EventContextUsage = "context_usage"
	EventError        = "error"
)

// Stop/finish reason constants shared by the OpenAI and Anthropic formatters.
const (
	StopEndTurn = "end_turn"
	StopToolUse = "tool_use"
	FinishStop  = "stop"
	FinishTools = "tool_calls"
	firstTokMsg = "first token timeout"
)

// FirstTokenTimeoutError is returned when the first chunk is not received
// within the configured timeout.
type FirstTokenTimeoutError struct{}

// Error implements the error interface.
func (e *FirstTokenTimeoutError) Error() string { return firstTokMsg }

// Config controls how the raw Kiro stream is parsed into events.
type Config struct {
	FakeReasoning         bool
	FakeReasoningHandling ThinkingHandling
	InitialBufferSize     int
	FirstTokenTimeout     time.Duration
}

const readBufSize = 4096

// ParseKiroStream reads the Kiro API response body and returns a channel of
// KiroEvent values. The goroutine closes the channel when the body is fully
// consumed, on error, or when the context is canceled.
func ParseKiroStream(ctx context.Context, body io.ReadCloser, cfg Config) <-chan KiroEvent {
	ch := make(chan KiroEvent, 16)

	go func() {
		defer close(ch)
		defer func() {
			if err := body.Close(); err != nil {
				slog.Debug("error closing stream body", "error", err)
			}
		}()

		parser := kiro.NewAwsEventStreamParser()

		var thinkingParser *ThinkingParser
		if cfg.FakeReasoning {
			bufSize := cfg.InitialBufferSize
			if bufSize <= 0 {
				bufSize = 20
			}
			thinkingParser = NewThinkingParser(cfg.FakeReasoningHandling, bufSize)
			slog.Debug("thinking parser initialized", "handling", cfg.FakeReasoningHandling)
		}

		firstChunk, err := readFirstChunk(ctx, body, cfg.FirstTokenTimeout)
		if err != nil {
			send(ctx, ch, KiroEvent{Type: EventError, Error: &FirstTokenTimeoutError{}})
			return
		}
		if len(firstChunk) == 0 {
			slog.Debug("empty response from Kiro API")
			return
		}

		processChunk(ctx, ch, parser, thinkingParser, string(firstChunk))

		// Subsequent reads — no special timeout.
		buf := make([]byte, readBufSize)
		for {
			n, readErr := body.Read(buf)
			if n > 0 {
				processChunk(ctx, ch, parser, thinkingParser, string(buf[:n]))
			}
			if readErr != nil {
				if readErr != io.EOF {
					slog.Error("error reading stream body", "error", readErr)
				}
				break
			}
		}

		finalizeThinking(ctx, ch, thinkingParser)
		emitToolCalls(ctx, ch, parser)
	}()

	return ch
}

// readFirstChunk reads the first chunk with a timeout. Returns the chunk data,
// or a non-nil error for timeout. An empty slice means empty response (not an error).
func readFirstChunk(ctx context.Context, body io.ReadCloser, timeout time.Duration) ([]byte, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	type readResult struct {
		data []byte
		err  error
	}

	resultCh := make(chan readResult, 1)
	go func() {
		buf := make([]byte, readBufSize)
		n, err := body.Read(buf)
		resultCh <- readResult{data: buf[:n], err: err}
	}()

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case <-timeoutCtx.Done():
		slog.Warn("first token timeout", "timeout", timeout)
		return nil, timeoutCtx.Err()
	case r := <-resultCh:
		if r.err != nil && r.err != io.EOF {
			return r.data, nil // partial data is still valid
		}
		return r.data, nil
	}
}

// processChunk feeds a chunk through the parser and emits events on the channel.
func processChunk(
	ctx context.Context,
	ch chan<- KiroEvent,
	parser *kiro.AwsEventStreamParser,
	thinkingParser *ThinkingParser,
	chunk string,
) {
	events := parser.Feed([]byte(chunk))
	for _, evt := range events {
		processParserEvent(ctx, ch, thinkingParser, evt)
	}
}

// processParserEvent converts a single kiro.ParserEvent to one or more KiroEvents.
func processParserEvent(
	ctx context.Context,
	ch chan<- KiroEvent,
	thinkingParser *ThinkingParser,
	evt kiro.ParserEvent,
) {
	switch evt.Type {
	case EventContent:
		content, _ := evt.Data.(string)
		processContentEvent(ctx, ch, thinkingParser, content)
	case EventUsage:
		usageMap, _ := evt.Data.(map[string]any)
		send(ctx, ch, KiroEvent{Type: EventUsage, Usage: usageMap})
	case EventContextUsage:
		pct, _ := evt.Data.(float64)
		send(ctx, ch, KiroEvent{Type: EventContextUsage, ContextUsagePercent: pct})
	}
}

// processContentEvent handles content events, optionally routing through the
// thinking parser.
func processContentEvent(
	ctx context.Context,
	ch chan<- KiroEvent,
	thinkingParser *ThinkingParser,
	content string,
) {
	if thinkingParser == nil {
		send(ctx, ch, KiroEvent{Type: EventContent, Content: content})
		return
	}

	result := thinkingParser.Feed(content)
	emitThinkingResult(ctx, ch, thinkingParser, result)
}

// emitThinkingResult sends events derived from a ThinkingParseResult.
func emitThinkingResult(
	ctx context.Context,
	ch chan<- KiroEvent,
	thinkingParser *ThinkingParser,
	result *ThinkingParseResult,
) {
	if result.ThinkingContent != "" {
		processed := thinkingParser.ProcessForOutput(
			result.ThinkingContent,
			result.IsFirstThinking,
			result.IsLastThinking,
		)
		if processed != nil {
			send(ctx, ch, KiroEvent{
				Type:                 EventThinking,
				ThinkingContent:      *processed,
				IsFirstThinkingChunk: result.IsFirstThinking,
				IsLastThinkingChunk:  result.IsLastThinking,
			})
		}
	}

	if result.RegularContent != "" {
		send(ctx, ch, KiroEvent{Type: EventContent, Content: result.RegularContent})
	}
}

// finalizeThinking flushes the thinking parser and emits any remaining content.
func finalizeThinking(ctx context.Context, ch chan<- KiroEvent, thinkingParser *ThinkingParser) {
	if thinkingParser == nil {
		return
	}

	result := thinkingParser.Finalize()
	emitThinkingResult(ctx, ch, thinkingParser, result)
}

// emitToolCalls sends tool_use events for all collected tool calls.
func emitToolCalls(ctx context.Context, ch chan<- KiroEvent, parser *kiro.AwsEventStreamParser) {
	for _, tc := range parser.GetToolCalls() {
		send(ctx, ch, KiroEvent{
			Type: EventToolUse,
			ToolUse: &ToolUseEvent{
				ID:                 tc.ID,
				Name:               tc.Name,
				Arguments:          tc.Arguments,
				TruncationDetected: tc.TruncationDetected,
			},
		})
	}
}

// send writes an event to the channel, respecting context cancellation.
func send(ctx context.Context, ch chan<- KiroEvent, evt KiroEvent) {
	select {
	case <-ctx.Done():
	case ch <- evt:
	}
}
