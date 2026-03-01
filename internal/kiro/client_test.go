package kiro

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// mock tokenProvider
// ---------------------------------------------------------------------------

type mockAuth struct {
	token              string
	fingerprint        string
	forceRefreshCalled atomic.Int32
	forceRefreshErr    error
	getTokenErr        error
}

func (m *mockAuth) GetAccessToken(_ context.Context) (string, error) {
	if m.getTokenErr != nil {
		return "", m.getTokenErr
	}
	return m.token, nil
}

func (m *mockAuth) ForceRefresh(_ context.Context) (string, error) {
	m.forceRefreshCalled.Add(1)
	if m.forceRefreshErr != nil {
		return "", m.forceRefreshErr
	}
	return m.token, nil
}

func (m *mockAuth) Fingerprint() string {
	return m.fingerprint
}

// newTestClient builds a HTTPClient with a mock auth provider and a no-op
// sleep function so tests run instantly.
func newTestClient(ma *mockAuth) *HTTPClient {
	return &HTTPClient{
		auth:                 ma,
		sharedClient:         &http.Client{Timeout: 5 * time.Second},
		streamingReadTimeout: 5 * time.Second,
		sleepFn:              func(time.Duration) {}, // no-op for fast tests
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRequestWithRetry_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(&mockAuth{token: "tok", fingerprint: "fp"})

	resp, err := c.RequestWithRetry(context.Background(), srv.URL, map[string]any{"msg": "hi"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"ok":true}` {
		t.Errorf("body = %q, want %q", body, `{"ok":true}`)
	}
}

func TestRequestWithRetry_403ThenSuccess(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("forbidden"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	ma := &mockAuth{token: "tok", fingerprint: "fp"}
	c := newTestClient(ma)

	resp, err := c.RequestWithRetry(context.Background(), srv.URL, map[string]any{}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if ma.forceRefreshCalled.Load() != 1 {
		t.Errorf("ForceRefresh called %d times, want 1", ma.forceRefreshCalled.Load())
	}
}

func TestRequestWithRetry_429ThenSuccess(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(&mockAuth{token: "tok", fingerprint: "fp"})

	resp, err := c.RequestWithRetry(context.Background(), srv.URL, map[string]any{}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if total := calls.Load(); total != 2 {
		t.Errorf("total calls = %d, want 2", total)
	}
}

func TestRequestWithRetry_500ThenSuccess(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("server error"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(&mockAuth{token: "tok", fingerprint: "fp"})

	resp, err := c.RequestWithRetry(context.Background(), srv.URL, map[string]any{}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestRequestWithRetry_NonRetryableStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer srv.Close()

	c := newTestClient(&mockAuth{token: "tok", fingerprint: "fp"})

	resp, err := c.RequestWithRetry(context.Background(), srv.URL, map[string]any{}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestRequestWithRetry_NetworkError_Retryable(t *testing.T) {
	// Point at a closed server to trigger a connection refused error.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := newTestClient(&mockAuth{token: "tok", fingerprint: "fp"})

	resp, err := c.RequestWithRetry(context.Background(), url, map[string]any{}, false)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected error, got nil")
	}

	if resp != nil {
		t.Error("expected nil response on network error")
	}
}

func TestRequestWithRetry_NetworkError_NonRetryable(t *testing.T) {
	// Use an invalid URL that triggers a TLS/certificate error classification.
	// We use a server that immediately closes to force a connection error,
	// but for non-retryable we need an SSL-type error. We'll use an HTTPS
	// server with an intentionally invalid cert.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Use a plain http.Client (no TLS config) against an HTTPS server to
	// trigger a certificate error, which ClassifyNetworkError maps to SSL
	// (not retryable).
	c := &HTTPClient{
		auth:         &mockAuth{token: "tok", fingerprint: "fp"},
		sharedClient: &http.Client{Timeout: 2 * time.Second},
		sleepFn:      func(time.Duration) {},
	}

	resp, err := c.RequestWithRetry(context.Background(), srv.URL, map[string]any{}, false)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected error for SSL, got nil")
	}
}

func TestRequestWithRetry_AllAttemptsExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("always failing"))
	}))
	defer srv.Close()

	c := newTestClient(&mockAuth{token: "tok", fingerprint: "fp"})

	resp, err := c.RequestWithRetry(context.Background(), srv.URL, map[string]any{}, false)
	if err == nil {
		if resp != nil {
			_ = resp.Body.Close()
		}
		t.Fatal("expected error after exhausting retries")
	}
	if resp != nil {
		t.Error("expected nil response when all attempts exhausted")
	}
	if strings.Contains(err.Error(), "%!w(<nil>)") {
		t.Fatalf("error should include concrete cause, got: %v", err)
	}
	if !strings.Contains(err.Error(), "retryable upstream status 500") {
		t.Fatalf("error should include final upstream status cause, got: %v", err)
	}
}

func TestRequestWithRetry_GetAccessTokenError(t *testing.T) {
	ma := &mockAuth{
		token:       "",
		fingerprint: "fp",
		getTokenErr: fmt.Errorf("auth: token expired"),
	}
	c := newTestClient(ma)

	resp, err := c.RequestWithRetry(context.Background(), "http://localhost:1", map[string]any{}, false)
	if err == nil {
		if resp != nil {
			_ = resp.Body.Close()
		}
		t.Fatal("expected error when GetAccessToken fails")
	}
}

func TestRequestWithRetry_StreamingUsesCloseHeader(t *testing.T) {
	var receivedConnection string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedConnection = r.Header.Get("Connection")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(&mockAuth{token: "tok", fingerprint: "fp"})

	resp, err := c.RequestWithRetry(context.Background(), srv.URL, map[string]any{}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if receivedConnection != "close" {
		t.Errorf("Connection header = %q, want %q", receivedConnection, "close")
	}
}

func TestPickClient_StreamingUsesConfiguredTimeout(t *testing.T) {
	c := &HTTPClient{
		auth:                 &mockAuth{token: "tok", fingerprint: "fp"},
		sharedClient:         &http.Client{Timeout: 10 * time.Second},
		streamingReadTimeout: 123 * time.Second,
		sleepFn:              func(time.Duration) {},
	}

	client := c.pickClient(true)
	if client.Timeout != 123*time.Second {
		t.Fatalf("streaming client timeout = %v, want %v", client.Timeout, 123*time.Second)
	}
}

func TestRequestWithRetry_BackoffIsCalled(t *testing.T) {
	var calls atomic.Int32
	var sleepCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := &HTTPClient{
		auth:         &mockAuth{token: "tok", fingerprint: "fp"},
		sharedClient: &http.Client{Timeout: 5 * time.Second},
		sleepFn: func(_ time.Duration) {
			sleepCalls.Add(1)
		},
	}

	resp, err := c.RequestWithRetry(context.Background(), srv.URL, map[string]any{}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if sleepCalls.Load() < 2 {
		t.Errorf("sleepFn called %d times, want at least 2", sleepCalls.Load())
	}
}

func TestRequestWithRetry_403ForceRefreshError(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	ma := &mockAuth{
		token:           "tok",
		fingerprint:     "fp",
		forceRefreshErr: fmt.Errorf("refresh failed"),
	}
	c := newTestClient(ma)

	// Even if ForceRefresh fails, the client should still retry.
	resp, err := c.RequestWithRetry(context.Background(), srv.URL, map[string]any{}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if ma.forceRefreshCalled.Load() != 1 {
		t.Errorf("ForceRefresh called %d times, want 1", ma.forceRefreshCalled.Load())
	}
}
