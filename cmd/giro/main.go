// Package main is the entry point for the giro server.
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/miltonparedes/giro/internal/auth"
	"github.com/miltonparedes/giro/internal/config"
	"github.com/miltonparedes/giro/internal/kiro"
	"github.com/miltonparedes/giro/internal/model"
	"github.com/miltonparedes/giro/internal/server"
)

func main() {
	cfg := config.Load()

	if err := cfg.Validate(); err != nil {
		slog.Error("config validation failed", "err", err)
		os.Exit(1)
	}

	var level slog.Level
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	cfg.PropagateVPNProxy()

	resolved, err := auth.ResolveSource(auth.ResolveInput{
		KiroCLIDBFile: cfg.KiroCLIDBFile,
		KiroCredsFile: cfg.KiroCredsFile,
		RefreshToken:  cfg.RefreshToken,
	})
	if err != nil {
		slog.Error("credential resolution failed", "err", err)
		os.Exit(1)
	}
	slog.Info("credential source resolved",
		"source", string(resolved.Kind),
		"path", resolved.Path,
	)

	sharedClient := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        config.MaxIdleConns,
			MaxIdleConnsPerHost: config.MaxIdleConnsPerHost,
			IdleConnTimeout:     config.IdleConnTimeout,
		},
		Timeout: 300 * time.Second, //nolint:gosec // long timeout for streaming
	}

	authManager, err := auth.NewKiroAuthManager(
		resolved.BuildAuthOptions(cfg.RefreshToken, cfg.ProfileARN, cfg.KiroRegion, cfg.VPNProxyURL),
	)
	if err != nil {
		slog.Error("failed to create auth manager", "err", err)
		os.Exit(1)
	}

	modelCache := model.NewInfoCache(time.Duration(config.ModelCacheTTL) * time.Second)

	ctx := context.Background()
	fetchModels(ctx, authManager, modelCache, sharedClient)

	for displayName, kiroID := range config.HiddenModels {
		modelCache.AddHiddenModel(displayName, kiroID)
	}

	modelResolver := model.NewResolver(modelCache, config.HiddenModels, config.ModelAliases, config.HiddenFromList)

	router := server.New(cfg, authManager, modelResolver, sharedClient)

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("starting server", "addr", srv.Addr, "version", config.AppVersion)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "err", err)
	}
	sharedClient.CloseIdleConnections()
	slog.Info("server stopped")
}

// fetchModels retrieves the list of available models from the Kiro API and
// updates the model cache. Falls back to a hardcoded list on failure.
func fetchModels(ctx context.Context, authManager *auth.KiroAuthManager, cache *model.InfoCache, client *http.Client) {
	token, err := authManager.GetAccessToken(ctx)
	if err != nil {
		slog.Warn("failed to get token for model fetch, using fallback models", "err", err)
		populateFallbackModels(cache)
		return
	}

	fetchURL := authManager.QHost() + "/ListAvailableModels?origin=AI_EDITOR"
	if authManager.GetAuthType() == auth.KiroDesktop && authManager.GetProfileARN() != "" {
		fetchURL += "&profileArn=" + authManager.GetProfileARN()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		slog.Warn("failed to build model fetch request", "err", err)
		populateFallbackModels(cache)
		return
	}

	headers := kiro.GetKiroHeaders(authManager.Fingerprint(), token)
	for k, v := range headers {
		req.Header[k] = v
	}

	resp, err := client.Do(req) //nolint:bodyclose,gosec // URL from trusted config; closed below
	if err != nil {
		slog.Warn("failed to fetch models, using fallback", "err", err)
		populateFallbackModels(cache)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("model fetch returned non-200", "status", resp.StatusCode) //nolint:gosec // status code is safe to log
		populateFallbackModels(cache)
		return
	}

	var result struct {
		Models []struct {
			ModelID     string `json:"modelId"`
			TokenLimits struct {
				MaxInputTokens int `json:"maxInputTokens"`
			} `json:"tokenLimits"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Warn("failed to parse models response", "err", err)
		populateFallbackModels(cache)
		return
	}

	models := make([]model.Info, 0, len(result.Models))
	for _, m := range result.Models {
		models = append(models, model.Info{
			ModelID:        m.ModelID,
			MaxInputTokens: m.TokenLimits.MaxInputTokens,
		})
	}
	cache.Update(models)
	slog.Info("fetched models from Kiro API", "count", len(models))
}

// populateFallbackModels loads the hardcoded fallback model list into the cache.
func populateFallbackModels(cache *model.InfoCache) {
	models := make([]model.Info, 0, len(config.FallbackModels))
	for _, id := range config.FallbackModels {
		models = append(models, model.Info{
			ModelID:        id,
			MaxInputTokens: config.DefaultMaxInputTokens,
		})
	}
	cache.Update(models)
	slog.Info("loaded fallback models", "count", len(models))
}
