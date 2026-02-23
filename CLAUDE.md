# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is giro

API gateway that translates OpenAI and Anthropic API formats to the Kiro API (AWS CodeWhisperer / Amazon Q Developer). Lets you use Claude models from Kiro with any OpenAI/Anthropic-compatible client.

## Commands

```sh
just dev        # go run ./cmd/giro
just build      # go build -o bin/giro ./cmd/giro
just test       # go test -race -count=1 ./...
just fmt        # gofumpt -w .
just lint       # golangci-lint run
just lint-fix   # golangci-lint run --fix
just check      # fmt + lint + test (use before committing)
just tidy       # go mod tidy && go mod verify
just cover      # test with coverage → coverage.html
```

Run a single test: `go test -race -run TestName ./internal/pkg/...`

## Architecture

```
cmd/giro/main.go           → entry point, slog setup, graceful shutdown (SIGINT/SIGTERM, 10s timeout)
internal/config/config.go  → env var config: HOST, PORT, LOG_LEVEL, API_KEY
internal/server/server.go  → chi router + middleware (RequestID, RealIP, Logger, Recoverer)
internal/handler/           → HTTP handlers (currently just health.go)
```

- **Router**: chi v5 — only external dependency. Standard `net/http` compatible.
- **Config**: plain `os.Getenv` with defaults, no config libraries.
- **Logging**: `log/slog` (stdlib structured logging), level set via `LOG_LEVEL` env var.
- **Formatter**: gofumpt (stricter than gofmt). **Linter**: golangci-lint v2.

## Code conventions

- `internal/` for all non-main packages — nothing is exported outside the module.
- Handlers are plain `http.HandlerFunc` signatures, registered on the chi mux in `server.New()`.
- Config is a simple struct passed by value; `cfg.Addr()` returns `host:port`.
- All exported types and functions have doc comments (revive linter enforces this).
- Error return values must be checked (errcheck). Use `_ = fn()` only when intentionally discarding.
- gosec is enabled: set `ReadHeaderTimeout` on `http.Server`, annotate false positives with `//nolint:gosec`.
- goimports groups: stdlib, external, then `github.com/miltonparedes/giro` (enforced by .golangci.yml).
- Test files are excluded from goconst, gosec, and unparam linters.
