// Package stream handles Kiro event stream parsing and SSE formatting.
package stream

import "strings"

// ThinkingState represents the FSM state of the thinking parser.
type ThinkingState int

const (
	// PreContent is the initial state, buffering to detect an opening tag.
	PreContent ThinkingState = iota
	// InThinking means the parser is inside a thinking block.
	InThinking
	// Streaming means regular content is flowing through.
	Streaming
)

// ThinkingHandling controls how thinking content is processed for output.
type ThinkingHandling string

const (
	// HandlingAsReasoning returns thinking content as-is for the reasoning_content field.
	HandlingAsReasoning ThinkingHandling = "as_reasoning_content"
	// HandlingRemove discards thinking content entirely.
	HandlingRemove ThinkingHandling = "remove"
	// HandlingPass re-wraps thinking content with the original tags.
	HandlingPass ThinkingHandling = "pass"
	// HandlingStripTags returns thinking content without tags.
	HandlingStripTags ThinkingHandling = "strip_tags"
)

// ThinkingParseResult holds the result of processing a content chunk.
type ThinkingParseResult struct {
	ThinkingContent string // thinking content to emit
	RegularContent  string // regular content to emit
	IsFirstThinking bool   // first thinking chunk
	IsLastThinking  bool   // last thinking chunk (closing tag found)
}

// ThinkingParser is a finite state machine that detects and separates thinking
// blocks from regular content in streaming responses. It detects thinking tags
// only at the start of the response and uses cautious buffering to handle tags
// split across chunks.
type ThinkingParser struct {
	state             ThinkingState
	handling          ThinkingHandling
	openTag           string
	closeTag          string
	initialBuffer     string
	thinkingBuffer    string
	maxTagLength      int
	initialBufferSize int
	supportedTags     []string
	isFirstThinking   bool
}

// NewThinkingParser creates a ThinkingParser with the given handling mode and
// initial buffer size for tag detection.
func NewThinkingParser(handling ThinkingHandling, initialBufferSize int) *ThinkingParser {
	tags := []string{"<thinking>", "<think>", "<reasoning>", "<thought>"}

	maxLen := 0
	for _, tag := range tags {
		if len(tag) > maxLen {
			maxLen = len(tag)
		}
	}

	return &ThinkingParser{
		state:             PreContent,
		handling:          handling,
		supportedTags:     tags,
		maxTagLength:      maxLen * 2, // len("</reasoning>") * 2 = 24
		initialBufferSize: initialBufferSize,
		isFirstThinking:   true,
	}
}

// Feed processes a chunk of content through the parser and returns any
// thinking or regular content that should be emitted.
func (p *ThinkingParser) Feed(content string) *ThinkingParseResult {
	if content == "" {
		return &ThinkingParseResult{}
	}

	switch p.state {
	case PreContent:
		return p.handlePreContent(content)
	case InThinking:
		return p.handleInThinking(content)
	default: // Streaming
		return &ThinkingParseResult{RegularContent: content}
	}
}

// Finalize flushes any remaining buffered content when the stream ends.
func (p *ThinkingParser) Finalize() *ThinkingParseResult {
	result := &ThinkingParseResult{}

	if p.thinkingBuffer != "" {
		if p.state == InThinking {
			result.ThinkingContent = p.thinkingBuffer
			result.IsFirstThinking = p.isFirstThinking
			result.IsLastThinking = true
		} else {
			result.RegularContent = p.thinkingBuffer
		}
		p.thinkingBuffer = ""
	}

	if p.initialBuffer != "" {
		result.RegularContent += p.initialBuffer
		p.initialBuffer = ""
	}

	return result
}

// ProcessForOutput applies the handling mode to thinking content.
// Returns nil when the content should be discarded.
func (p *ThinkingParser) ProcessForOutput(content string, isFirst, isLast bool) *string {
	if content == "" {
		return nil
	}

	switch p.handling {
	case HandlingRemove:
		return nil
	case HandlingPass:
		var prefix, suffix string
		if isFirst && p.openTag != "" {
			prefix = p.openTag
		}
		if isLast && p.closeTag != "" {
			suffix = p.closeTag
		}
		s := prefix + content + suffix
		return &s
	default: // HandlingAsReasoning, HandlingStripTags
		return &content
	}
}

func (p *ThinkingParser) handlePreContent(content string) *ThinkingParseResult {
	p.initialBuffer += content
	stripped := strings.TrimLeft(p.initialBuffer, " \t\n\r")

	for _, tag := range p.supportedTags {
		if strings.HasPrefix(stripped, tag) {
			p.state = InThinking
			p.openTag = tag
			p.closeTag = "</" + tag[1:]

			afterTag := stripped[len(tag):]
			p.thinkingBuffer = afterTag
			p.initialBuffer = ""

			return p.processThinkingBuffer()
		}
	}

	for _, tag := range p.supportedTags {
		if strings.HasPrefix(tag, stripped) && len(stripped) < len(tag) {
			return &ThinkingParseResult{}
		}
	}

	// No tag found and buffer either exceeds limit or doesn't match any prefix.
	if len(p.initialBuffer) > p.initialBufferSize || !p.couldBeTagPrefix(stripped) {
		p.state = Streaming
		result := &ThinkingParseResult{RegularContent: p.initialBuffer}
		p.initialBuffer = ""
		return result
	}

	return &ThinkingParseResult{}
}

func (p *ThinkingParser) couldBeTagPrefix(text string) bool {
	if text == "" {
		return true
	}
	for _, tag := range p.supportedTags {
		if strings.HasPrefix(tag, text) {
			return true
		}
	}
	return false
}

func (p *ThinkingParser) handleInThinking(content string) *ThinkingParseResult {
	p.thinkingBuffer += content
	return p.processThinkingBuffer()
}

func (p *ThinkingParser) processThinkingBuffer() *ThinkingParseResult {
	result := &ThinkingParseResult{}

	if p.closeTag == "" {
		return result
	}

	if idx := strings.Index(p.thinkingBuffer, p.closeTag); idx >= 0 {
		thinking := p.thinkingBuffer[:idx]
		afterTag := p.thinkingBuffer[idx+len(p.closeTag):]

		if thinking != "" {
			result.ThinkingContent = thinking
			result.IsFirstThinking = p.isFirstThinking
			p.isFirstThinking = false
		}

		result.IsLastThinking = true
		p.state = Streaming
		p.thinkingBuffer = ""

		if afterTag != "" {
			stripped := strings.TrimLeft(afterTag, " \t\n\r")
			if stripped != "" {
				result.RegularContent = stripped
			}
		}

		return result
	}

	// No closing tag yet — cautious buffering.
	if len(p.thinkingBuffer) > p.maxTagLength {
		sendUpTo := len(p.thinkingBuffer) - p.maxTagLength
		result.ThinkingContent = p.thinkingBuffer[:sendUpTo]
		result.IsFirstThinking = p.isFirstThinking
		p.isFirstThinking = false
		p.thinkingBuffer = p.thinkingBuffer[sendUpTo:]
	}

	return result
}
