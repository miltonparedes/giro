package kiro_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/miltonparedes/giro/internal/kiro"
)

// ---------------------------------------------------------------------------
// ClassifyNetworkError
// ---------------------------------------------------------------------------

func TestClassifyNetworkError_DNS(t *testing.T) {
	info := kiro.ClassifyNetworkError(errors.New("dial tcp: lookup api.example.com: no such host"))
	assertCategory(t, info, kiro.ErrDNS, true, 502)
}

func TestClassifyNetworkError_ConnectionRefused(t *testing.T) {
	info := kiro.ClassifyNetworkError(errors.New("dial tcp 127.0.0.1:443: connection refused"))
	assertCategory(t, info, kiro.ErrConnRefused, true, 502)
}

func TestClassifyNetworkError_ConnectionReset(t *testing.T) {
	info := kiro.ClassifyNetworkError(errors.New("read tcp: connection reset by peer"))
	assertCategory(t, info, kiro.ErrConnReset, true, 502)
}

func TestClassifyNetworkError_NetworkUnreachable(t *testing.T) {
	info := kiro.ClassifyNetworkError(errors.New("dial tcp: network is unreachable"))
	assertCategory(t, info, kiro.ErrNetUnreachable, true, 502)
}

func TestClassifyNetworkError_TimeoutConnect(t *testing.T) {
	info := kiro.ClassifyNetworkError(errors.New("dial tcp 1.2.3.4:443: i/o timeout"))
	assertCategory(t, info, kiro.ErrTimeoutConnect, true, 504)
}

func TestClassifyNetworkError_TimeoutRead(t *testing.T) {
	info := kiro.ClassifyNetworkError(errors.New("read: context deadline exceeded (Client.Timeout exceeded while reading body)"))
	assertCategory(t, info, kiro.ErrTimeoutRead, true, 504)
}

func TestClassifyNetworkError_SSL(t *testing.T) {
	info := kiro.ClassifyNetworkError(errors.New("tls: failed to verify certificate"))
	assertCategory(t, info, kiro.ErrSSL, false, 502)
}

func TestClassifyNetworkError_SSLCertificate(t *testing.T) {
	info := kiro.ClassifyNetworkError(errors.New("x509: certificate signed by unknown authority"))
	assertCategory(t, info, kiro.ErrSSL, false, 502)
}

func TestClassifyNetworkError_Proxy(t *testing.T) {
	info := kiro.ClassifyNetworkError(errors.New("proxy connection failed: 503 Service Unavailable"))
	assertCategory(t, info, kiro.ErrProxy, true, 502)
}

func TestClassifyNetworkError_Redirect(t *testing.T) {
	info := kiro.ClassifyNetworkError(errors.New("stopped after 10 redirects"))
	assertCategory(t, info, kiro.ErrRedirect, false, 502)
}

func TestClassifyNetworkError_Unknown(t *testing.T) {
	info := kiro.ClassifyNetworkError(errors.New("something completely unexpected"))
	assertCategory(t, info, kiro.ErrUnknown, true, 502)
}

func TestClassifyNetworkError_HasUserMessage(t *testing.T) {
	info := kiro.ClassifyNetworkError(errors.New("no such host"))
	if info.UserMessage == "" {
		t.Error("expected non-empty UserMessage")
	}
}

func TestClassifyNetworkError_HasTroubleshootSteps(t *testing.T) {
	info := kiro.ClassifyNetworkError(errors.New("connection refused"))
	if len(info.TroubleshootSteps) == 0 {
		t.Error("expected non-empty TroubleshootSteps")
	}
}

func TestClassifyNetworkError_PreservesTechnicalDetails(t *testing.T) {
	original := "dial tcp 1.2.3.4:443: connection refused"
	info := kiro.ClassifyNetworkError(errors.New(original))
	if info.TechnicalDetails != original {
		t.Errorf("TechnicalDetails = %q, want %q", info.TechnicalDetails, original)
	}
}

// ---------------------------------------------------------------------------
// EnhanceKiroError
// ---------------------------------------------------------------------------

func TestEnhanceKiroError_ContentLength(t *testing.T) {
	info := kiro.EnhanceKiroError(map[string]any{
		"message": "content length exceeds threshold",
		"reason":  "CONTENT_LENGTH_EXCEEDS_THRESHOLD",
	})
	if info.UserMessage != "Model context limit reached. Conversation size exceeds model capacity." {
		t.Errorf("unexpected UserMessage: %q", info.UserMessage)
	}
	if info.OriginalMessage != "content length exceeds threshold" {
		t.Errorf("unexpected OriginalMessage: %q", info.OriginalMessage)
	}
}

func TestEnhanceKiroError_MonthlyRequestCount(t *testing.T) {
	info := kiro.EnhanceKiroError(map[string]any{
		"message": "monthly request limit",
		"reason":  "MONTHLY_REQUEST_COUNT",
	})
	if info.UserMessage != "Monthly request limit exceeded. Account has reached its monthly quota." {
		t.Errorf("unexpected UserMessage: %q", info.UserMessage)
	}
}

func TestEnhanceKiroError_UnknownReason(t *testing.T) {
	info := kiro.EnhanceKiroError(map[string]any{
		"message": "something went wrong",
		"reason":  "THROTTLED",
	})
	want := "something went wrong (reason: THROTTLED)"
	if info.UserMessage != want {
		t.Errorf("UserMessage = %q, want %q", info.UserMessage, want)
	}
}

func TestEnhanceKiroError_NoReason(t *testing.T) {
	info := kiro.EnhanceKiroError(map[string]any{
		"message": "generic error",
	})
	if info.UserMessage != "generic error" {
		t.Errorf("UserMessage = %q, want %q", info.UserMessage, "generic error")
	}
	if info.Reason != "" {
		t.Errorf("Reason = %q, want empty", info.Reason)
	}
}

func TestEnhanceKiroError_ReasonUNKNOWN(t *testing.T) {
	info := kiro.EnhanceKiroError(map[string]any{
		"message": "some error",
		"reason":  "UNKNOWN",
	})
	if info.UserMessage != "some error" {
		t.Errorf("UserMessage = %q, want %q", info.UserMessage, "some error")
	}
}

// ---------------------------------------------------------------------------
// FormatErrorForOpenAI
// ---------------------------------------------------------------------------

func TestFormatErrorForOpenAI(t *testing.T) {
	result := kiro.FormatErrorForOpenAI("test error", 500)

	errObj, ok := result["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error object")
	}

	if errObj["message"] != "test error" {
		t.Errorf("message = %q, want %q", errObj["message"], "test error")
	}
	if errObj["type"] != "kiro_api_error" {
		t.Errorf("type = %q, want %q", errObj["type"], "kiro_api_error")
	}
	if errObj["code"] != 500 {
		t.Errorf("code = %v, want %v", errObj["code"], 500)
	}
}

// ---------------------------------------------------------------------------
// FormatErrorForAnthropic
// ---------------------------------------------------------------------------

func TestFormatErrorForAnthropic(t *testing.T) {
	result := kiro.FormatErrorForAnthropic("test error")

	if result["type"] != "error" {
		t.Errorf("type = %q, want %q", result["type"], "error")
	}

	errObj, ok := result["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error object")
	}

	if errObj["type"] != "api_error" {
		t.Errorf("error.type = %q, want %q", errObj["type"], "api_error")
	}
	if errObj["message"] != "test error" {
		t.Errorf("error.message = %q, want %q", errObj["message"], "test error")
	}
}

// ---------------------------------------------------------------------------
// KiroHTTPError helpers
// ---------------------------------------------------------------------------

func TestStatusCodeFromError_Direct(t *testing.T) {
	status, ok := kiro.StatusCodeFromError(&kiro.KiroHTTPError{
		StatusCode: 401,
		Message:    "unauthorized",
	})

	if !ok {
		t.Fatal("expected status to be found")
	}
	if status != 401 {
		t.Fatalf("status = %d, want 401", status)
	}
}

func TestStatusCodeFromError_Wrapped(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &kiro.KiroHTTPError{
		StatusCode: 429,
		Message:    "rate limit",
	})

	status, ok := kiro.StatusCodeFromError(err)
	if !ok {
		t.Fatal("expected status to be found")
	}
	if status != 429 {
		t.Fatalf("status = %d, want 429", status)
	}
}

func TestStatusCodeFromError_Unknown(t *testing.T) {
	_, ok := kiro.StatusCodeFromError(errors.New("plain error"))
	if ok {
		t.Fatal("expected status lookup to fail")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func assertCategory(t *testing.T, info kiro.NetworkErrorInfo, wantCat kiro.ErrorCategory, wantRetryable bool, wantCode int) {
	t.Helper()
	if info.Category != wantCat {
		t.Errorf("Category = %q, want %q", info.Category, wantCat)
	}
	if info.IsRetryable != wantRetryable {
		t.Errorf("IsRetryable = %v, want %v", info.IsRetryable, wantRetryable)
	}
	if info.SuggestedHTTPCode != wantCode {
		t.Errorf("SuggestedHTTPCode = %d, want %d", info.SuggestedHTTPCode, wantCode)
	}
}
