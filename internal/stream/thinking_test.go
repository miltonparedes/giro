package stream

import "testing"

func TestThinking_NoThinkingTag(t *testing.T) {
	p := NewThinkingParser(HandlingAsReasoning, 20)

	r := p.Feed("Hello, world!")
	// Buffer exceeded without tag match → flush as regular content.
	if r.RegularContent != "Hello, world!" {
		t.Fatalf("expected regular content %q, got %q", "Hello, world!", r.RegularContent)
	}
	if r.ThinkingContent != "" {
		t.Fatalf("expected no thinking content, got %q", r.ThinkingContent)
	}
}

func TestThinking_FullThinkingBlock(t *testing.T) {
	p := NewThinkingParser(HandlingAsReasoning, 20)

	// Feed the entire block in one call.
	r := p.Feed("<thinking>deep thought</thinking>response text")

	// Thinking content emitted (may include partial due to cautious buffering).
	// Collect everything via finalize too.
	allThinking := r.ThinkingContent
	allRegular := r.RegularContent

	fin := p.Finalize()
	allThinking += fin.ThinkingContent
	allRegular += fin.RegularContent

	if allThinking != "deep thought" {
		t.Fatalf("expected thinking %q, got %q", "deep thought", allThinking)
	}
	if allRegular != "response text" {
		t.Fatalf("expected regular %q, got %q", "response text", allRegular)
	}
}

func TestThinking_AllOpeningTags(t *testing.T) {
	tags := []struct {
		open  string
		close string
	}{
		{"<think>", "</think>"},
		{"<reasoning>", "</reasoning>"},
		{"<thought>", "</thought>"},
	}

	for _, tc := range tags {
		p := NewThinkingParser(HandlingAsReasoning, 20)

		input := tc.open + "inner" + tc.close + "after"
		r := p.Feed(input)

		allThinking := r.ThinkingContent
		allRegular := r.RegularContent

		fin := p.Finalize()
		allThinking += fin.ThinkingContent
		allRegular += fin.RegularContent

		if allThinking != "inner" {
			t.Errorf("tag %s: expected thinking %q, got %q", tc.open, "inner", allThinking)
		}
		if allRegular != "after" {
			t.Errorf("tag %s: expected regular %q, got %q", tc.open, "after", allRegular)
		}
	}
}

func TestThinking_StreamedTag(t *testing.T) {
	p := NewThinkingParser(HandlingAsReasoning, 20)

	// Tag arrives in chunks.
	r1 := p.Feed("<thi")
	if r1.RegularContent != "" && r1.ThinkingContent != "" {
		t.Fatalf("expected buffering, got regular=%q thinking=%q", r1.RegularContent, r1.ThinkingContent)
	}

	r2 := p.Feed("nking>hello</thinking>world")
	allThinking := r2.ThinkingContent
	allRegular := r2.RegularContent

	fin := p.Finalize()
	allThinking += fin.ThinkingContent
	allRegular += fin.RegularContent

	if allThinking != "hello" {
		t.Fatalf("expected thinking %q, got %q", "hello", allThinking)
	}
	if allRegular != "world" {
		t.Fatalf("expected regular %q, got %q", "world", allRegular)
	}
}

func TestThinking_BufferExceeded(t *testing.T) {
	p := NewThinkingParser(HandlingAsReasoning, 20)

	// Feed more than 20 chars without a tag.
	r := p.Feed("this is regular content that exceeds buffer")
	if r.RegularContent != "this is regular content that exceeds buffer" {
		t.Fatalf("expected flushed regular content, got %q", r.RegularContent)
	}
	if p.state != Streaming {
		t.Fatalf("expected Streaming state, got %d", p.state)
	}
}

func TestThinking_TagPrefix(t *testing.T) {
	p := NewThinkingParser(HandlingAsReasoning, 20)

	// "<th" is a prefix of "<thinking>" and "<thought>" — keep buffering.
	r := p.Feed("<th")
	if r.RegularContent != "" || r.ThinkingContent != "" {
		t.Fatalf("expected buffering, got regular=%q thinking=%q", r.RegularContent, r.ThinkingContent)
	}
	if p.state != PreContent {
		t.Fatalf("expected PreContent, got %d", p.state)
	}
}

func TestThinking_WhitespaceBeforeTag(t *testing.T) {
	p := NewThinkingParser(HandlingAsReasoning, 40)

	r := p.Feed("  \n <thinking>thought</thinking>rest")

	allThinking := r.ThinkingContent
	allRegular := r.RegularContent

	fin := p.Finalize()
	allThinking += fin.ThinkingContent
	allRegular += fin.RegularContent

	if allThinking != "thought" {
		t.Fatalf("expected thinking %q, got %q", "thought", allThinking)
	}
	if allRegular != "rest" {
		t.Fatalf("expected regular %q, got %q", "rest", allRegular)
	}
}

func TestThinking_ContentAfterClosingTag(t *testing.T) {
	p := NewThinkingParser(HandlingAsReasoning, 20)

	r := p.Feed("<thinking>t</thinking>After closing tag")

	allRegular := r.RegularContent

	fin := p.Finalize()
	allRegular += fin.RegularContent

	if allRegular != "After closing tag" {
		t.Fatalf("expected regular %q, got %q", "After closing tag", allRegular)
	}
}

func TestThinking_ContentAfterTag_WhitespaceStripped(t *testing.T) {
	p := NewThinkingParser(HandlingAsReasoning, 20)

	r := p.Feed("<thinking>t</thinking>  \n  hello")

	allRegular := r.RegularContent
	fin := p.Finalize()
	allRegular += fin.RegularContent

	if allRegular != "hello" {
		t.Fatalf("expected regular %q (whitespace stripped), got %q", "hello", allRegular)
	}
}

func TestThinking_Handling_AsReasoning(t *testing.T) {
	p := NewThinkingParser(HandlingAsReasoning, 20)
	p.openTag = "<thinking>"
	p.closeTag = "</thinking>"

	out := p.ProcessForOutput("some thought", true, false)
	if out == nil || *out != "some thought" {
		t.Fatalf("expected %q, got %v", "some thought", out)
	}
}

func TestThinking_Handling_Remove(t *testing.T) {
	p := NewThinkingParser(HandlingRemove, 20)
	p.openTag = "<thinking>"
	p.closeTag = "</thinking>"

	out := p.ProcessForOutput("some thought", true, true)
	if out != nil {
		t.Fatalf("expected nil, got %q", *out)
	}
}

func TestThinking_Handling_Pass(t *testing.T) {
	p := NewThinkingParser(HandlingPass, 20)
	p.openTag = "<thinking>"
	p.closeTag = "</thinking>"

	out := p.ProcessForOutput("some thought", true, true)
	if out == nil {
		t.Fatal("expected non-nil output")
	}
	expected := "<thinking>some thought</thinking>"
	if *out != expected {
		t.Fatalf("expected %q, got %q", expected, *out)
	}
}

func TestThinking_Handling_StripTags(t *testing.T) {
	p := NewThinkingParser(HandlingStripTags, 20)
	p.openTag = "<thinking>"
	p.closeTag = "</thinking>"

	out := p.ProcessForOutput("some thought", true, true)
	if out == nil || *out != "some thought" {
		t.Fatalf("expected %q, got %v", "some thought", out)
	}
}

func TestThinking_Finalize(t *testing.T) {
	p := NewThinkingParser(HandlingAsReasoning, 20)

	// Feed tag but never close it.
	p.Feed("<thinking>unfinished thought")

	fin := p.Finalize()
	if fin.ThinkingContent == "" {
		t.Fatal("expected finalize to flush thinking content")
	}
	if !fin.IsLastThinking {
		t.Fatal("expected IsLastThinking on finalize")
	}

	// Also test finalize in PreContent state.
	p2 := NewThinkingParser(HandlingAsReasoning, 50)
	p2.Feed("<thi") // partial tag, still buffering
	fin2 := p2.Finalize()
	if fin2.RegularContent != "<thi" {
		t.Fatalf("expected initial buffer flushed as regular, got %q", fin2.RegularContent)
	}
}

func TestThinking_CautiousBuffering(t *testing.T) {
	p := NewThinkingParser(HandlingAsReasoning, 20)

	// Enter thinking state.
	p.Feed("<thinking>")

	// Feed content longer than maxTagLength (24).
	r := p.Feed("abcdefghijklmnopqrstuvwxyz0123456789")
	if r.ThinkingContent == "" {
		t.Fatal("expected some thinking content to be emitted via cautious buffering")
	}

	// The thinking buffer should retain exactly maxTagLength chars.
	if len(p.thinkingBuffer) != p.maxTagLength {
		t.Fatalf("expected buffer len %d, got %d", p.maxTagLength, len(p.thinkingBuffer))
	}
}
