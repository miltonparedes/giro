package truncation

import (
	"fmt"
	"sync"
	"testing"
)

func TestRecordAndGetToolTruncation(t *testing.T) {
	s := NewState()

	info := map[string]any{
		"reason":     "missing 2 closing braces",
		"size_bytes": 5000,
	}
	s.RecordToolTruncation("call_1", "write_to_file", info)

	// First retrieval should return the entry.
	got := s.GetToolTruncation("call_1")
	if got == nil {
		t.Fatal("expected non-nil ToolTruncation on first get")
	}
	if got.ToolUseID != "call_1" {
		t.Errorf("ToolUseID = %q, want %q", got.ToolUseID, "call_1")
	}
	if got.ToolName != "write_to_file" {
		t.Errorf("ToolName = %q, want %q", got.ToolName, "write_to_file")
	}
	if got.TruncationInfo["reason"] != "missing 2 closing braces" {
		t.Errorf("reason = %v, want %q", got.TruncationInfo["reason"], "missing 2 closing braces")
	}

	// Second retrieval should return nil (one-time pop).
	if second := s.GetToolTruncation("call_1"); second != nil {
		t.Error("expected nil on second get (pop semantics)")
	}
}

func TestGetToolTruncation_NotFound(t *testing.T) {
	s := NewState()

	if got := s.GetToolTruncation("nonexistent"); got != nil {
		t.Errorf("expected nil for missing key, got %+v", got)
	}
}

func TestMultipleToolTruncations(t *testing.T) {
	s := NewState()

	tools := []struct {
		id   string
		name string
	}{
		{"call_a", "read_file"},
		{"call_b", "write_to_file"},
		{"call_c", "execute_command"},
	}

	for _, tc := range tools {
		s.RecordToolTruncation(tc.id, tc.name, map[string]any{"reason": "test"})
	}

	// Retrieve in reverse order.
	for i := len(tools) - 1; i >= 0; i-- {
		tc := tools[i]
		got := s.GetToolTruncation(tc.id)
		if got == nil {
			t.Fatalf("expected entry for %q", tc.id)
		}
		if got.ToolName != tc.name {
			t.Errorf("ToolName = %q, want %q", got.ToolName, tc.name)
		}
	}

	// All should now be gone.
	for _, tc := range tools {
		if got := s.GetToolTruncation(tc.id); got != nil {
			t.Errorf("expected nil after pop for %q", tc.id)
		}
	}
}

func TestRecordAndGetContentTruncation(t *testing.T) {
	s := NewState()

	content := "This is truncated content that was cut off mid-sentence and never completed properly..."
	s.RecordContentTruncation(content)

	// First retrieval with same content should succeed.
	got := s.GetContentTruncation(content)
	if got == nil {
		t.Fatal("expected non-nil ContentTruncation on first get")
	}
	if got.MessageHash == "" {
		t.Error("MessageHash should not be empty")
	}

	// Second retrieval should return nil (one-time pop).
	if second := s.GetContentTruncation(content); second != nil {
		t.Error("expected nil on second get (pop semantics)")
	}
}

func TestGetContentTruncation_DifferentContent(t *testing.T) {
	s := NewState()

	s.RecordContentTruncation("hello world")

	if got := s.GetContentTruncation("different content"); got != nil {
		t.Errorf("expected nil for non-matching content, got %+v", got)
	}
}

func TestContentHash_First500Chars(t *testing.T) {
	s := NewState()

	// Two strings that share the same first 500 characters but differ after.
	prefix := make([]byte, 500)
	for i := range prefix {
		prefix[i] = 'A'
	}
	contentA := string(prefix) + "BBBB"
	contentB := string(prefix) + "CCCC"

	s.RecordContentTruncation(contentA)

	// Retrieving with contentB (same first 500 chars) should match.
	got := s.GetContentTruncation(contentB)
	if got == nil {
		t.Fatal("expected match for content sharing the same first 500 characters")
	}
}

func TestContentHash_ShortContent(t *testing.T) {
	s := NewState()

	short := "hi"
	s.RecordContentTruncation(short)

	got := s.GetContentTruncation(short)
	if got == nil {
		t.Fatal("expected non-nil for short content")
	}
}

func TestClear(t *testing.T) {
	s := NewState()

	s.RecordToolTruncation("id1", "tool1", map[string]any{"reason": "test"})
	s.RecordContentTruncation("some content")

	s.Clear()

	if got := s.GetToolTruncation("id1"); got != nil {
		t.Error("expected nil tool truncation after Clear")
	}
	if got := s.GetContentTruncation("some content"); got != nil {
		t.Error("expected nil content truncation after Clear")
	}
}

func TestConcurrentAccess(t *testing.T) {
	s := NewState()
	const n = 50

	var wg sync.WaitGroup
	wg.Add(n * 2) // n tool + n content goroutines

	// Concurrent tool truncation writes and reads.
	for i := range n {
		id := fmt.Sprintf("tool_%d", i)
		go func() {
			defer wg.Done()
			s.RecordToolTruncation(id, "tool_"+id, map[string]any{"i": i})
		}()
		go func() {
			defer wg.Done()
			// May or may not find the entry depending on scheduling.
			_ = s.GetToolTruncation(id)
		}()
	}

	wg.Wait()

	// Concurrent content truncation writes and reads.
	wg.Add(n * 2)
	for i := range n {
		content := fmt.Sprintf("content_%d_padding", i)
		go func() {
			defer wg.Done()
			s.RecordContentTruncation(content)
		}()
		go func() {
			defer wg.Done()
			_ = s.GetContentTruncation(content)
		}()
	}

	wg.Wait()

	// No panics or data races means success (run with -race).
	t.Log("concurrent access completed without race conditions")
}
