package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/miltonparedes/giro/internal/auth"
	"github.com/miltonparedes/giro/internal/config"
	"github.com/miltonparedes/giro/internal/convert"
	"github.com/miltonparedes/giro/internal/kiro"
	"github.com/miltonparedes/giro/internal/model"
	"github.com/miltonparedes/giro/internal/stream"
	"github.com/miltonparedes/giro/internal/types"
)

// AnthropicHandler serves the Anthropic-compatible API endpoints.
type AnthropicHandler struct {
	authManager   *auth.KiroAuthManager
	modelResolver *model.Resolver
	sharedClient  *http.Client
	cfg           config.Config
}

// NewAnthropicHandler creates an AnthropicHandler with the given dependencies.
func NewAnthropicHandler(
	authManager *auth.KiroAuthManager,
	resolver *model.Resolver,
	sharedClient *http.Client,
	cfg config.Config,
) *AnthropicHandler {
	return &AnthropicHandler{
		authManager:   authManager,
		modelResolver: resolver,
		sharedClient:  sharedClient,
		cfg:           cfg,
	}
}

// Messages handles POST /v1/messages (Anthropic Messages API).
func (h *AnthropicHandler) Messages(w http.ResponseWriter, r *http.Request) {
	var req types.AnthropicMessagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, kiro.FormatErrorForAnthropic("invalid request body: "+err.Error()))
		return
	}

	conversationID := uuid.NewString()

	profileARN := ""
	if h.authManager.GetAuthType() == auth.KiroDesktop && h.authManager.GetProfileARN() != "" {
		profileARN = h.authManager.GetProfileARN()
	}

	resolution := h.modelResolver.Resolve(req.Model)
	resolvedModel := resolution.ResolvedModel
	if resolution.InternalID != "" {
		resolvedModel = resolution.InternalID
	}

	convCfg := convert.Config{
		FakeReasoning:            h.cfg.FakeReasoning,
		FakeReasoningMaxTokens:   h.cfg.FakeReasoningMaxTokens,
		TruncationRecovery:       h.cfg.TruncationRecovery,
		ToolDescriptionMaxLength: h.cfg.ToolDescriptionMaxLength,
	}
	payloadResult, err := convert.AnthropicToCorePayload(&req, resolvedModel, conversationID, profileARN, convCfg)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, kiro.FormatErrorForAnthropic(err.Error()))
		return
	}

	events, err := h.doKiroRequest(r.Context(), payloadResult.Payload)
	if err != nil {
		writeJSONError(w, kiroErrorStatus(err), kiro.FormatErrorForAnthropic(err.Error()))
		return
	}

	anthropicCfg := stream.AnthropicStreamConfig{
		Model:            resolution.ResolvedModel,
		ThinkingHandling: stream.ThinkingHandling(h.cfg.FakeReasoningHandling),
	}

	if req.Stream {
		h.streamAnthropicResponse(w, events, anthropicCfg)
	} else {
		h.collectAnthropicResponse(w, events, anthropicCfg)
	}
}

// streamAnthropicResponse writes SSE events to the response writer.
func (h *AnthropicHandler) streamAnthropicResponse(w http.ResponseWriter, events <-chan stream.KiroEvent, cfg stream.AnthropicStreamConfig) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, kiro.FormatErrorForAnthropic("streaming not supported"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	for chunk := range stream.FormatAnthropicSSE(events, cfg) {
		_, _ = fmt.Fprint(w, chunk)
		flusher.Flush()
	}
}

// collectAnthropicResponse builds a non-streaming JSON response.
func (h *AnthropicHandler) collectAnthropicResponse(w http.ResponseWriter, events <-chan stream.KiroEvent, cfg stream.AnthropicStreamConfig) {
	resp, err := stream.CollectAnthropicResponse(events, cfg)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, kiro.FormatErrorForAnthropic(err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// doKiroRequest sends a request to the Kiro API with first-token retry logic.
// On success it returns the event channel and nil error. The response body is
// owned by ParseKiroStream, which closes it when the stream is fully consumed.
func (h *AnthropicHandler) doKiroRequest(ctx context.Context, payload map[string]any) (<-chan stream.KiroEvent, error) {
	streamCfg := stream.Config{
		FakeReasoning:         h.cfg.FakeReasoning,
		FakeReasoningHandling: stream.ThinkingHandling(h.cfg.FakeReasoningHandling),
		InitialBufferSize:     h.cfg.FakeReasoningInitialBufferSize,
		FirstTokenTimeout:     time.Duration(h.cfg.FirstTokenTimeout * float64(time.Second)),
	}

	url := h.authManager.APIHost() + "/generateAssistantResponse"
	client := kiro.NewHTTPClient(
		h.authManager,
		h.sharedClient,
		time.Duration(h.cfg.StreamingReadTimeout*float64(time.Second)),
	)

	maxRetries := h.cfg.FirstTokenMaxRetries
	if maxRetries < 1 {
		maxRetries = 1
	}

	for attempt := range maxRetries {
		resp, err := client.RequestWithRetry(ctx, url, payload, true) //nolint:bodyclose // body is closed by ParseKiroStream
		if err != nil {
			return nil, fmt.Errorf("kiro request failed: %w", err)
		}

		// Non-200: read error body and return formatted error.
		if resp.StatusCode != http.StatusOK {
			return nil, h.handleKiroErrorResponse(resp)
		}

		events := stream.ParseKiroStream(ctx, resp.Body, streamCfg)

		// Peek at first event to check for timeout.
		firstEvent, ok := <-events
		if !ok {
			// Empty channel, retry.
			slog.Warn("empty event stream from Kiro", "attempt", attempt+1)
			continue
		}

		if firstEvent.Error != nil {
			if _, isTimeout := firstEvent.Error.(*stream.FirstTokenTimeoutError); isTimeout {
				slog.Warn("first token timeout, retrying", "attempt", attempt+1)
				continue
			}
			return nil, firstEvent.Error
		}

		// Re-emit the first event through a new channel.
		reEmit := make(chan stream.KiroEvent, 100)
		go func() {
			defer close(reEmit)
			reEmit <- firstEvent
			for ev := range events {
				reEmit <- ev
			}
		}()

		return reEmit, nil
	}

	return nil, fmt.Errorf("all %d first-token retry attempts failed", maxRetries)
}

// handleKiroErrorResponse reads a non-200 Kiro response and returns a
// descriptive error. The response body is fully consumed and closed.
func (h *AnthropicHandler) handleKiroErrorResponse(resp *http.Response) error {
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	var errorJSON map[string]any
	if err := json.Unmarshal(body, &errorJSON); err == nil {
		enhanced := kiro.EnhanceKiroError(errorJSON)
		return &kiro.HTTPError{
			StatusCode: resp.StatusCode,
			Message:    enhanced.UserMessage,
		}
	}

	return &kiro.HTTPError{
		StatusCode: resp.StatusCode,
		Message:    string(body),
	}
}
