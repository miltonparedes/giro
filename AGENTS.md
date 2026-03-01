# Repository Guidelines

## Project Structure & Module Organization
- `cmd/giro/main.go`: application entrypoint (startup, logging, graceful shutdown).
- `internal/`: gateway implementation packages (`auth`, `config`, `convert`, `handler`, `kiro`, `middleware`, `model`, `server`, `stream`, `truncation`, `types`).
- Tests live next to code as `*_test.go` files (for example, `internal/stream/core_test.go`).
- Build artifacts are generated under `bin/` and coverage outputs at `coverage.out` / `coverage.html`.

## Build, Test, and Development Commands
- `just dev`: run the gateway locally (`go run ./cmd/giro`).
- `just build`: compile binary to `bin/giro`.
- `just test`: run all tests with race detector (`go test -race -count=1 ./...`).
- `just cover`: generate coverage report and `coverage.html`.
- `just fmt`: format all Go code with `gofumpt`.
- `just lint` / `just lint-fix`: run `golangci-lint` (check or auto-fix).
- `just check`: run `fmt`, `lint`, and `test` before opening a PR.

## Coding Style & Naming Conventions
- Use idiomatic Go with tabs (default `gofumpt` behavior) and keep packages focused by domain.
- Prefer lowercase package names and descriptive file names by feature (for example, `anthropic.go`, `openai.go`).
- Keep `internal/` boundaries intact; avoid exporting APIs unless required.
- Use `goimports` ordering (stdlib, third-party, local module `github.com/miltonparedes/giro`).
- Ensure exported identifiers have doc comments (`revive` is enabled).

## Testing Guidelines
- Use Go’s standard `testing` package with table-driven tests where helpful.
- Name tests `TestXxx` and keep them in the same package directory as target code.
- Run `just test` for normal validation and `just cover` when changing request/stream conversion behavior.
- There is no hard coverage gate currently; new logic should include targeted tests for success and error paths.

## Commit & Pull Request Guidelines
- Follow Conventional Commits used in history: `feat: ...`, `fix: ...`, `chore: ...`.
- Keep commits scoped and atomic; include tests with behavior changes.
- PRs should include: clear summary, linked issue (if any), test evidence (`just check` output), and sample request/response notes when API behavior changes.

## Security & Configuration Tips
- Configure runtime via environment variables: `HOST`, `PORT`, `LOG_LEVEL`, `API_KEY`.
- Never commit real API keys; use local env files or shell exports.
- Keep `ReadHeaderTimeout`/HTTP safety defaults intact when touching server setup.
