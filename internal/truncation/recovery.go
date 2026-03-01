package truncation

import "fmt"

// GenerateTruncationToolResult builds a human-readable notice explaining that
// the tool call identified by toolUseID was truncated by the upstream API.
//
// truncationInfo is expected to carry "reason" (string) and "size_bytes"
// (numeric) keys. Missing or zero-valued entries are replaced with sensible
// defaults.
func GenerateTruncationToolResult(toolName, toolUseID string, truncationInfo map[string]any) string {
	reason := "unknown"
	if v, ok := truncationInfo["reason"]; ok {
		if s, ok := v.(string); ok && s != "" {
			reason = s
		}
	}

	var sizeBytes int64
	switch v := truncationInfo["size_bytes"].(type) {
	case int:
		sizeBytes = int64(v)
	case int64:
		sizeBytes = v
	case float64:
		sizeBytes = int64(v)
	}

	return fmt.Sprintf(
		"[API Limitation] Your tool call '%s' (id: %s) was truncated by the API.\n"+
			"Truncation details: %s, received %d bytes.\n"+
			"\n"+
			"IMPORTANT: Your tool call arguments were cut off by the API (not by the user).\n"+
			"The tool could NOT execute because the arguments were incomplete.\n"+
			"Please retry the tool call, potentially with shorter arguments or a simpler approach.",
		toolName, toolUseID, reason, sizeBytes,
	)
}

// GenerateTruncationUserMessage returns a synthetic user-role message that
// informs the model its previous response was cut off by the API.
func GenerateTruncationUserMessage() string {
	return "[System Notice] Your previous response was cut off by the API before it could complete.\n" +
		"This is an API limitation, not a user interruption.\n" +
		"Please continue from where you left off, or summarize and retry with a more concise approach."
}
