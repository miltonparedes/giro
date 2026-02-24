// Package server sets up the HTTP router and middleware.
package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/miltonparedes/giro/internal/auth"
	"github.com/miltonparedes/giro/internal/config"
	"github.com/miltonparedes/giro/internal/handler"
	"github.com/miltonparedes/giro/internal/middleware"
	"github.com/miltonparedes/giro/internal/model"
)

// New creates a chi router with standard middleware and all API routes.
func New(
	cfg config.Config,
	authManager *auth.KiroAuthManager,
	modelResolver *model.Resolver,
	sharedClient *http.Client,
) *chi.Mux {
	r := chi.NewRouter()

	r.Use(middleware.CORS())
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)

	// Health endpoints (no auth).
	r.Get("/", handler.Health)
	r.Get("/health", handler.DetailedHealth)

	openaiHandler := handler.NewOpenAIHandler(authManager, modelResolver, sharedClient, cfg)
	r.Route("/v1", func(sub chi.Router) {
		sub.Use(middleware.OpenAIAuth(cfg.ProxyAPIKey))
		sub.Get("/models", openaiHandler.Models)
		sub.Post("/chat/completions", openaiHandler.ChatCompletions)
	})

	anthropicHandler := handler.NewAnthropicHandler(authManager, modelResolver, sharedClient, cfg)
	r.With(middleware.AnthropicAuth(cfg.ProxyAPIKey)).Post("/v1/messages", anthropicHandler.Messages)

	return r
}
