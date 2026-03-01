package types

import (
	"crypto/rand"
	"encoding/hex"
)

func generateHex(n int) string {
	b := make([]byte, (n+1)/2)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)[:n]
}

// GenerateCompletionID returns an ID in the format "chatcmpl-{32 hex chars}".
func GenerateCompletionID() string {
	return "chatcmpl-" + generateHex(32)
}

// GenerateToolCallID returns an ID in the format "call_{8 hex chars}".
func GenerateToolCallID() string {
	return "call_" + generateHex(8)
}

// GenerateMessageID returns an ID in the format "msg_{24 hex chars}".
func GenerateMessageID() string {
	return "msg_" + generateHex(24)
}

// GenerateToolUseID returns an ID in the format "toolu_{24 hex chars}".
func GenerateToolUseID() string {
	return "toolu_" + generateHex(24)
}

// GenerateThinkingSignature returns an ID in the format "sig_{32 hex chars}".
func GenerateThinkingSignature() string {
	return "sig_" + generateHex(32)
}
