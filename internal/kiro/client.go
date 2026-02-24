package kiro

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/miltonparedes/giro/internal/auth"
	"github.com/miltonparedes/giro/internal/config"
)

// tokenProvider abstracts the auth operations needed by HTTPClient.
// *auth.KiroAuthManager satisfies this interface.
type tokenProvider interface {
	GetAccessToken(ctx context.Context) (string, error)
	ForceRefresh(ctx context.Context) (string, error)
	Fingerprint() string
}

// HTTPClient sends requests to the Kiro API with automatic retry,
// exponential backoff, and transparent token refresh on 403 responses.
type HTTPClient struct {
	auth         tokenProvider
	sharedClient *http.Client
	sleepFn      func(time.Duration) // injectable for testing
}

// NewHTTPClient creates an HTTPClient that uses authManager for
// authentication and sharedClient for non-streaming HTTP requests.
func NewHTTPClient(authManager *auth.KiroAuthManager, sharedClient *http.Client) *HTTPClient {
	return &HTTPClient{
		auth:         authManager,
		sharedClient: sharedClient,
		sleepFn:      time.Sleep,
	}
}

// RequestWithRetry sends a POST request to url with the JSON-encoded payload.
// It retries on 403 (after a forced token refresh), 429, and 5xx responses
// with exponential backoff. For streaming requests, each attempt uses a
// dedicated HTTP client with keep-alives disabled.
//
// The caller is responsible for closing the returned response body.
func (c *HTTPClient) RequestWithRetry(
	ctx context.Context,
	url string,
	payload map[string]any,
	streaming bool,
) (*http.Response, error) {
	maxRetries := config.MaxRetries

	var lastErr error

	for attempt := range maxRetries {
		resp, netErr := c.doRequest(ctx, url, payload, streaming)
		if netErr != nil {
			if shouldBreak := c.handleNetworkError(netErr, attempt, maxRetries); shouldBreak {
				lastErr = netErr
				break
			}
			lastErr = netErr
			continue
		}

		done, ret := c.handleResponse(ctx, resp, attempt, maxRetries)
		if done {
			return ret, nil
		}
	}

	return nil, fmt.Errorf("kiro: all %d attempts exhausted: %w", maxRetries, lastErr)
}

// doRequest executes a single HTTP POST to the Kiro API. It obtains a fresh
// token, builds headers, marshals the payload, and picks the appropriate HTTP
// client.
func (c *HTTPClient) doRequest(
	ctx context.Context,
	url string,
	payload map[string]any,
	streaming bool,
) (*http.Response, error) {
	token, err := c.auth.GetAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}

	headers := GetKiroHeaders(c.auth.Fingerprint(), token)
	if streaming {
		headers.Set("Connection", "close")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	for key, vals := range headers {
		for _, v := range vals {
			req.Header.Set(key, v)
		}
	}

	client := c.pickClient(streaming)

	return client.Do(req) //nolint:bodyclose,gosec // caller closes body; URL from trusted config
}

// pickClient returns a per-request client with keep-alives disabled for
// streaming, or the shared client for normal requests.
func (c *HTTPClient) pickClient(streaming bool) *http.Client {
	if !streaming {
		return c.sharedClient
	}
	return &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
	}
}

// handleResponse decides what to do after receiving an HTTP response. It
// returns (true, resp) when the caller should use the response, or
// (false, nil) when the loop should continue retrying.
func (c *HTTPClient) handleResponse(
	ctx context.Context,
	resp *http.Response,
	attempt, maxRetries int,
) (done bool, ret *http.Response) {
	switch {
	case resp.StatusCode == http.StatusOK:
		return true, resp

	case resp.StatusCode == http.StatusForbidden:
		_ = resp.Body.Close()
		slog.Warn("kiro: received 403, forcing token refresh", "attempt", attempt+1)
		if _, refreshErr := c.auth.ForceRefresh(ctx); refreshErr != nil {
			slog.Error("kiro: force refresh failed", "error", refreshErr)
		}
		return false, nil

	case resp.StatusCode == http.StatusTooManyRequests:
		_ = resp.Body.Close()
		c.backoff(attempt, maxRetries, resp.StatusCode)
		return false, nil

	case resp.StatusCode >= 500 && resp.StatusCode < 600:
		_ = resp.Body.Close()
		c.backoff(attempt, maxRetries, resp.StatusCode)
		return false, nil

	default:
		// Non-retryable status (e.g. 400, 422): let the caller handle it.
		return true, resp
	}
}

// handleNetworkError classifies a network error and decides whether to retry.
// It returns true when the retry loop should break immediately.
func (c *HTTPClient) handleNetworkError(err error, attempt, maxRetries int) (shouldBreak bool) {
	info := ClassifyNetworkError(err)
	slog.Error("kiro: network error",
		"category", info.Category,
		"message", info.UserMessage,
		"attempt", attempt+1,
		"retryable", info.IsRetryable,
	)

	if !info.IsRetryable {
		return true
	}

	if attempt+1 < maxRetries {
		c.backoff(attempt, maxRetries, 0)
	}

	return false
}

// backoff sleeps for an exponentially increasing duration.
func (c *HTTPClient) backoff(attempt, maxRetries, statusCode int) {
	delay := time.Duration(config.BaseRetryDelay*math.Pow(2, float64(attempt))) * time.Second

	slog.Info("kiro: retrying after backoff",
		"attempt", attempt+1,
		"max_retries", maxRetries,
		"delay", delay,
		"status_code", statusCode,
	)

	c.sleepFn(delay)
}
