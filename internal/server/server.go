// Package server sets up the HTTP router and middleware.
package server

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/miltonparedes/giro/internal/config"
	"github.com/miltonparedes/giro/internal/handler"
)

// New creates a chi router with standard middleware and routes.
func New(_ config.Config) *chi.Mux {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", handler.Health)

	return r
}
