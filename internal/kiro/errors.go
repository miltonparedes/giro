package kiro

import (
	"errors"
	"fmt"
	"strings"
)

// ErrorCategory classifies network errors for user-facing diagnostics.
type ErrorCategory string

// ErrorCategory values for network error classification.
const (
	ErrDNS            ErrorCategory = "dns_resolution"
	ErrConnRefused    ErrorCategory = "connection_refused"
	ErrConnReset      ErrorCategory = "connection_reset"
	ErrNetUnreachable ErrorCategory = "network_unreachable"
	ErrTimeoutConnect ErrorCategory = "timeout_connect"
	ErrTimeoutRead    ErrorCategory = "timeout_read"
	ErrSSL            ErrorCategory = "ssl_error"
	ErrProxy          ErrorCategory = "proxy_error"
	ErrRedirect       ErrorCategory = "too_many_redirects"
	ErrUnknown        ErrorCategory = "unknown"
)

// NetworkErrorInfo contains classified details about a network-level error,
// including user-facing messaging and retry guidance.
type NetworkErrorInfo struct {
	Category          ErrorCategory
	UserMessage       string
	TroubleshootSteps []string
	TechnicalDetails  string
	IsRetryable       bool
	SuggestedHTTPCode int
}

// ErrorInfo holds an enhanced Kiro API error with a user-friendly message.
type ErrorInfo struct {
	Reason          string
	UserMessage     string
	OriginalMessage string
}

// HTTPError represents an upstream Kiro API failure with its original
// HTTP status code and message.
type HTTPError struct {
	StatusCode int
	Message    string
}

// Error implements the error interface.
func (e *HTTPError) Error() string {
	return fmt.Sprintf("kiro API error (HTTP %d): %s", e.StatusCode, e.Message)
}

// StatusCodeFromError extracts an upstream HTTP status code from err when the
// error wraps or is a *HTTPError.
func StatusCodeFromError(err error) (int, bool) {
	var ke *HTTPError
	if !errors.As(err, &ke) {
		return 0, false
	}
	return ke.StatusCode, true
}

// ClassifyNetworkError inspects an error and returns a NetworkErrorInfo with
// the appropriate category, user message, troubleshooting steps, and retry
// guidance.
func ClassifyNetworkError(err error) NetworkErrorInfo {
	errStr := strings.ToLower(err.Error())

	info := NetworkErrorInfo{TechnicalDetails: err.Error()}

	switch {
	case containsAny(errStr, "no such host", "dns"):
		classifyDNS(&info)
	case containsAny(errStr, "connection refused", "econnrefused"):
		classifyConnRefused(&info)
	case containsAny(errStr, "connection reset", "econnreset"):
		classifyConnReset(&info)
	case containsAny(errStr, "network is unreachable", "no route to host"):
		classifyNetUnreachable(&info)
	case strings.Contains(errStr, "timeout") && containsAny(errStr, "dial", "connect"):
		classifyTimeoutConnect(&info)
	case strings.Contains(errStr, "timeout"):
		classifyTimeoutRead(&info)
	case containsAny(errStr, "tls", "ssl", "certificate"):
		classifySSL(&info)
	case strings.Contains(errStr, "proxy"):
		classifyProxy(&info)
	case containsAny(errStr, "redirect", "stopped after"):
		classifyRedirect(&info)
	default:
		classifyUnknown(&info)
	}

	return info
}

// EnhanceKiroError maps a Kiro API error JSON payload to a user-friendly
// ErrorInfo, enriching known reason codes with actionable messages.
func EnhanceKiroError(errorJSON map[string]any) ErrorInfo {
	message, _ := errorJSON["message"].(string)
	reason, _ := errorJSON["reason"].(string)

	info := ErrorInfo{
		Reason:          reason,
		OriginalMessage: message,
	}

	switch reason {
	case "CONTENT_LENGTH_EXCEEDS_THRESHOLD":
		info.UserMessage = "Model context limit reached. Conversation size exceeds model capacity."
	case "MONTHLY_REQUEST_COUNT":
		info.UserMessage = "Monthly request limit exceeded. Account has reached its monthly quota."
	default:
		info.UserMessage = message
		if reason != "" && reason != "UNKNOWN" {
			info.UserMessage = fmt.Sprintf("%s (reason: %s)", message, reason)
		}
	}

	return info
}

// FormatErrorForOpenAI returns an OpenAI-compatible error response body.
func FormatErrorForOpenAI(message string, code int) map[string]any {
	return map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "kiro_api_error",
			"code":    code,
		},
	}
}

// FormatErrorForAnthropic returns an Anthropic-compatible error response body.
func FormatErrorForAnthropic(message string) map[string]any {
	return map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "api_error",
			"message": message,
		},
	}
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func classifyDNS(info *NetworkErrorInfo) {
	info.Category = ErrDNS
	info.UserMessage = "DNS resolution failed. Cannot resolve the Kiro API hostname."
	info.TroubleshootSteps = []string{
		"Check your internet connection",
		"Verify DNS settings",
		"Try using a different DNS server (e.g. 8.8.8.8)",
	}
	info.IsRetryable = true
	info.SuggestedHTTPCode = 502
}

func classifyConnRefused(info *NetworkErrorInfo) {
	info.Category = ErrConnRefused
	info.UserMessage = "Connection refused by the Kiro API server."
	info.TroubleshootSteps = []string{
		"Check if the Kiro service is available",
		"Verify firewall settings",
		"Check if a VPN is required",
	}
	info.IsRetryable = true
	info.SuggestedHTTPCode = 502
}

func classifyConnReset(info *NetworkErrorInfo) {
	info.Category = ErrConnReset
	info.UserMessage = "Connection was reset by the Kiro API server."
	info.TroubleshootSteps = []string{
		"This is usually temporary, retrying may help",
		"Check network stability",
		"Verify proxy/VPN configuration",
	}
	info.IsRetryable = true
	info.SuggestedHTTPCode = 502
}

func classifyNetUnreachable(info *NetworkErrorInfo) {
	info.Category = ErrNetUnreachable
	info.UserMessage = "Network is unreachable. Cannot reach the Kiro API server."
	info.TroubleshootSteps = []string{
		"Check your internet connection",
		"Verify network configuration",
		"Check if a VPN is required",
	}
	info.IsRetryable = true
	info.SuggestedHTTPCode = 502
}

func classifyTimeoutConnect(info *NetworkErrorInfo) {
	info.Category = ErrTimeoutConnect
	info.UserMessage = "Connection to the Kiro API timed out."
	info.TroubleshootSteps = []string{
		"Check your internet connection speed",
		"The Kiro API may be experiencing high load",
		"Try again in a few moments",
	}
	info.IsRetryable = true
	info.SuggestedHTTPCode = 504
}

func classifyTimeoutRead(info *NetworkErrorInfo) {
	info.Category = ErrTimeoutRead
	info.UserMessage = "Kiro API response timed out."
	info.TroubleshootSteps = []string{
		"The request may have been too large",
		"The Kiro API may be experiencing high load",
		"Try again with a smaller request",
	}
	info.IsRetryable = true
	info.SuggestedHTTPCode = 504
}

func classifySSL(info *NetworkErrorInfo) {
	info.Category = ErrSSL
	info.UserMessage = "SSL/TLS error connecting to the Kiro API."
	info.TroubleshootSteps = []string{
		"Check your system clock is correct",
		"Verify SSL certificate configuration",
		"Check if a corporate proxy is intercepting HTTPS",
	}
	info.IsRetryable = false
	info.SuggestedHTTPCode = 502
}

func classifyProxy(info *NetworkErrorInfo) {
	info.Category = ErrProxy
	info.UserMessage = "Proxy error connecting to the Kiro API."
	info.TroubleshootSteps = []string{
		"Verify proxy configuration",
		"Check proxy server availability",
		"Try connecting without a proxy",
	}
	info.IsRetryable = true
	info.SuggestedHTTPCode = 502
}

func classifyRedirect(info *NetworkErrorInfo) {
	info.Category = ErrRedirect
	info.UserMessage = "Too many redirects when connecting to the Kiro API."
	info.TroubleshootSteps = []string{
		"Check proxy or VPN configuration",
		"Verify the API endpoint URL",
	}
	info.IsRetryable = false
	info.SuggestedHTTPCode = 502
}

func classifyUnknown(info *NetworkErrorInfo) {
	info.Category = ErrUnknown
	info.UserMessage = "An unexpected network error occurred connecting to the Kiro API."
	info.TroubleshootSteps = []string{
		"Check your internet connection",
		"Try again in a few moments",
		"Check the logs for more details",
	}
	info.IsRetryable = true
	info.SuggestedHTTPCode = 502
}
