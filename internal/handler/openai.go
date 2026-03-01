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

// OpenAIHandler serves the OpenAI-compatible API endpoints.
type OpenAIHandler struct {
	authManager   *auth.KiroAuthManager
	modelResolver *model.Resolver
	sharedClient  *http.Client
	cfg           config.Config
}

// NewOpenAIHandler creates an OpenAIHandler with the given dependencies.
func NewOpenAIHandler(
	authManager *auth.KiroAuthManager,
	resolver *model.Resolver,
	sharedClient *http.Client,
	cfg config.Config,
) *OpenAIHandler {
	return &OpenAIHandler{
		authManager:   authManager,
		modelResolver: resolver,
		sharedClient:  sharedClient,
		cfg:           cfg,
	}
}

// Models handles GET /v1/models and returns the list of available models.
func (h *OpenAIHandler) Models(w http.ResponseWriter, _ *http.Request) {
	ids := h.modelResolver.GetAvailableModels()
	now := time.Now().Unix()

	data := make([]types.OpenAIModel, 0, len(ids))
	for _, id := range ids {
		data = append(data, types.OpenAIModel{
			ID:      id,
			Object:  "model",
			Created: now,
			OwnedBy: "anthropic",
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(types.ModelList{
		Object: "list",
		Data:   data,
	})
}

// ChatCompletions handles POST /v1/chat/completions.
func (h *OpenAIHandler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req types.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, kiro.FormatErrorForOpenAI("invalid request body: "+err.Error(), http.StatusBadRequest))
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
	payloadResult, err := convert.OpenAIToCorePayload(&req, resolvedModel, conversationID, profileARN, convCfg)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, kiro.FormatErrorForOpenAI(err.Error(), http.StatusBadRequest))
		return
	}

	events, err := h.doKiroRequest(r.Context(), payloadResult.Payload)
	if err != nil {
		status := kiroErrorStatus(err)
		writeJSONError(w, status, kiro.FormatErrorForOpenAI(err.Error(), status))
		return
	}

	openAICfg := stream.OpenAIStreamConfig{
		Model:            resolution.ResolvedModel,
		ThinkingHandling: stream.ThinkingHandling(h.cfg.FakeReasoningHandling),
	}

	if req.Stream {
		h.streamOpenAIResponse(w, events, openAICfg)
	} else {
		h.collectOpenAIResponse(w, events, openAICfg)
	}
}

// streamOpenAIResponse writes SSE chunks to the response writer.
func (h *OpenAIHandler) streamOpenAIResponse(w http.ResponseWriter, events <-chan stream.KiroEvent, cfg stream.OpenAIStreamConfig) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, kiro.FormatErrorForOpenAI("streaming not supported", http.StatusInternalServerError))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	for chunk := range stream.FormatOpenAISSE(events, cfg) {
		_, _ = fmt.Fprint(w, chunk)
		flusher.Flush()
	}
}

// collectOpenAIResponse builds a non-streaming JSON response.
func (h *OpenAIHandler) collectOpenAIResponse(w http.ResponseWriter, events <-chan stream.KiroEvent, cfg stream.OpenAIStreamConfig) {
	resp, err := stream.CollectOpenAIResponse(events, cfg)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, kiro.FormatErrorForOpenAI(err.Error(), http.StatusBadGateway))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// doKiroRequest sends a request to the Kiro API with first-token retry logic.
// On success it returns the event channel and nil error. The response body is
// owned by ParseKiroStream, which closes it when the stream is fully consumed.
func (h *OpenAIHandler) doKiroRequest(ctx context.Context, payload map[string]any) (<-chan stream.KiroEvent, error) {
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
func (h *OpenAIHandler) handleKiroErrorResponse(resp *http.Response) error {
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
