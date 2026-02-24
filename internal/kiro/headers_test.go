package kiro_test

import (
	"strings"
	"testing"

	"github.com/miltonparedes/giro/internal/kiro"
)

func TestGetKiroHeaders_AllPresent(t *testing.T) {
	h := kiro.GetKiroHeaders("abc123", "tok-xyz")

	expected := []string{
		"Authorization",
		"Content-Type",
		"User-Agent",
		"X-Amz-User-Agent",
		"X-Amzn-Codewhisperer-Optout",
		"X-Amzn-Kiro-Agent-Mode",
		"Amz-Sdk-Invocation-Id",
		"Amz-Sdk-Request",
	}
	for _, key := range expected {
		if h.Get(key) == "" {
			t.Errorf("missing header %q", key)
		}
	}
}

func TestGetKiroHeaders_Authorization(t *testing.T) {
	h := kiro.GetKiroHeaders("fp", "my-token")
	if got := h.Get("Authorization"); got != "Bearer my-token" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer my-token")
	}
}

func TestGetKiroHeaders_FingerprintInUserAgent(t *testing.T) {
	fp := "deadbeef1234"
	h := kiro.GetKiroHeaders(fp, "tok")

	ua := h.Get("User-Agent")
	if !strings.Contains(ua, "KiroIDE-0.7.45-"+fp) {
		t.Errorf("User-Agent %q does not contain fingerprint %q", ua, fp)
	}

	amzUA := h.Get("X-Amz-User-Agent")
	if !strings.Contains(amzUA, "KiroIDE-0.7.45-"+fp) {
		t.Errorf("x-amz-user-agent %q does not contain fingerprint %q", amzUA, fp)
	}
}

func TestGetKiroHeaders_UniqueInvocationID(t *testing.T) {
	h1 := kiro.GetKiroHeaders("fp", "tok")
	h2 := kiro.GetKiroHeaders("fp", "tok")

	id1 := h1.Get("Amz-Sdk-Invocation-Id")
	id2 := h2.Get("Amz-Sdk-Invocation-Id")

	if id1 == "" {
		t.Fatal("amz-sdk-invocation-id is empty")
	}
	if id1 == id2 {
		t.Errorf("two calls returned same invocation ID %q", id1)
	}
}

func TestGetKiroHeaders_StaticValues(t *testing.T) {
	h := kiro.GetKiroHeaders("fp", "tok")

	tests := map[string]string{
		"Content-Type":                "application/json",
		"X-Amzn-Codewhisperer-Optout": "true",
		"X-Amzn-Kiro-Agent-Mode":      "vibe",
		"Amz-Sdk-Request":             "attempt=1; max=3",
	}
	for key, want := range tests {
		if got := h.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}
