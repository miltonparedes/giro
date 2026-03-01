package truncation

import (
	"strings"
	"testing"
)

func TestGenerateTruncationToolResult(t *testing.T) {
	info := map[string]any{
		"reason":     "missing 2 closing braces",
		"size_bytes": 5000,
	}

	result := GenerateTruncationToolResult("write_to_file", "call_xyz", info)

	checks := []string{
		"[API Limitation]",
		"write_to_file",
		"call_xyz",
		"missing 2 closing braces",
		"5000 bytes",
		"IMPORTANT",
		"could NOT execute",
		"retry the tool call",
	}
	for _, want := range checks {
		if !strings.Contains(result, want) {
			t.Errorf("result missing %q\ngot: %s", want, result)
		}
	}
}

func TestGenerateTruncationToolResult_Defaults(t *testing.T) {
	// Empty info map -- reason and size_bytes should fall back to defaults.
	result := GenerateTruncationToolResult("read_file", "call_empty", map[string]any{})

	if !strings.Contains(result, "unknown") {
		t.Error("expected default reason 'unknown' when key is missing")
	}
	if !strings.Contains(result, "0 bytes") {
		t.Error("expected '0 bytes' when size_bytes is missing")
	}
	if !strings.Contains(result, "read_file") {
		t.Error("expected tool name in output")
	}
	if !strings.Contains(result, "call_empty") {
		t.Error("expected tool use ID in output")
	}
}

func TestGenerateTruncationToolResult_NilInfo(t *testing.T) {
	result := GenerateTruncationToolResult("search", "call_nil", nil)

	if !strings.Contains(result, "unknown") {
		t.Error("expected default reason for nil info")
	}
	if !strings.Contains(result, "0 bytes") {
		t.Error("expected '0 bytes' for nil info")
	}
}

func TestGenerateTruncationToolResult_FloatSizeBytes(t *testing.T) {
	info := map[string]any{
		"reason":     "test",
		"size_bytes": float64(3500),
	}

	result := GenerateTruncationToolResult("tool", "id", info)

	if !strings.Contains(result, "3500 bytes") {
		t.Errorf("expected '3500 bytes' for float64 size, got: %s", result)
	}
}

func TestGenerateTruncationUserMessage(t *testing.T) {
	msg := GenerateTruncationUserMessage()

	checks := []string{
		"[System Notice]",
		"cut off by the API",
		"API limitation",
		"not a user interruption",
		"continue from where you left off",
	}
	for _, want := range checks {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q\ngot: %s", want, msg)
		}
	}
}

func TestGenerateTruncationUserMessage_Deterministic(t *testing.T) {
	a := GenerateTruncationUserMessage()
	b := GenerateTruncationUserMessage()

	if a != b {
		t.Error("expected deterministic output across calls")
	}
}
