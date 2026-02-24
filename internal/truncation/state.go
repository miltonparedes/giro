// Package truncation provides thread-safe tracking and recovery for tool calls
// and content that are truncated by the upstream Kiro API.
package truncation

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// ToolTruncation stores information about a truncated tool call.
type ToolTruncation struct {
	ToolName       string
	ToolUseID      string
	TruncationInfo map[string]any // e.g. {"reason": "...", "size_bytes": 5000, "is_truncated": true}
}

// ContentTruncation stores information about truncated content.
type ContentTruncation struct {
	MessageHash string
}

// State provides thread-safe storage for truncation tracking.
//
// Tool calls are keyed by their stable tool_use_id. Content is keyed by a
// SHA-256 hash of the first 500 characters. Both use a pop-on-read pattern:
// entries are deleted after the first retrieval.
type State struct {
	mu              sync.Mutex
	toolTruncations map[string]*ToolTruncation
	contentHashes   map[string]*ContentTruncation
}

// NewState returns an initialised State ready for use.
func NewState() *State {
	return &State{
		toolTruncations: make(map[string]*ToolTruncation),
		contentHashes:   make(map[string]*ContentTruncation),
	}
}

// RecordToolTruncation saves truncation information for a tool call identified
// by toolUseID. If an entry already exists for the same ID it is overwritten.
func (s *State) RecordToolTruncation(toolUseID, toolName string, info map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.toolTruncations[toolUseID] = &ToolTruncation{
		ToolName:       toolName,
		ToolUseID:      toolUseID,
		TruncationInfo: info,
	}
}

// GetToolTruncation returns and removes the truncation record for toolUseID.
// It returns nil when no record exists (one-time retrieval).
func (s *State) GetToolTruncation(toolUseID string) *ToolTruncation {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.toolTruncations[toolUseID]
	if !ok {
		return nil
	}
	delete(s.toolTruncations, toolUseID)
	return t
}

// RecordContentTruncation stores a hash derived from the first 500 characters
// of content so that a later request can detect the truncation.
func (s *State) RecordContentTruncation(content string) {
	h := contentHash(content)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.contentHashes[h] = &ContentTruncation{MessageHash: h}
}

// GetContentTruncation checks whether content was previously recorded as
// truncated. If found the entry is removed and the ContentTruncation is
// returned; otherwise nil is returned (one-time retrieval).
func (s *State) GetContentTruncation(content string) *ContentTruncation {
	h := contentHash(content)

	s.mu.Lock()
	defer s.mu.Unlock()

	ct, ok := s.contentHashes[h]
	if !ok {
		return nil
	}
	delete(s.contentHashes, h)
	return ct
}

// Clear removes all recorded truncation state.
func (s *State) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.toolTruncations = make(map[string]*ToolTruncation)
	s.contentHashes = make(map[string]*ContentTruncation)
}

// contentHash returns the hex-encoded SHA-256 of the first 500 characters (or
// fewer if content is shorter).
func contentHash(content string) string {
	end := len(content)
	if end > 500 {
		end = 500
	}
	sum := sha256.Sum256([]byte(content[:end]))
	return hex.EncodeToString(sum[:])
}
