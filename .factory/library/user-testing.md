# User Testing

Testing surface findings, required tools, and runtime constraints for validation.

**What belongs here:** validation surfaces, concrete local validation setup, concurrency guidance, runtime gotchas.
**What does NOT belong here:** implementation details better captured in architecture notes.

---

## Validation Surface

### Surface: local startup and API validation
- Primary tools: `shell` and `curl`
- Entry point: local `giro` process on `127.0.0.1:8080`
- Real credential source for validation: `~/.local/share/kiro-cli/data.sqlite3`
- Required local auth for client-facing APIs during validation: `PROXY_API_KEY=giro-validation-key`

### Required matrix
Run against one live process whenever the milestone/feature requires full-path validation:
1. `GET /health`
2. `GET /v1/models`
3. OpenAI non-stream
4. OpenAI stream
5. Anthropic non-stream
6. Anthropic stream
7. negative client auth
8. tool use
9. vision

### Concrete local boot command
`HOST=127.0.0.1 PORT=8080 PROXY_API_KEY=giro-validation-key KIRO_CLI_DB_FILE=$HOME/.local/share/kiro-cli/data.sqlite3 go run ./cmd/giro`

### Validation notes
- `/health` must remain unauthenticated.
- `/v1/models` and `/v1/chat/completions` use OpenAI-style auth/error contracts.
- `/v1/messages` uses Anthropic-style auth/error contracts and accepts `x-api-key` or Bearer.
- Advanced paths must be validated through the live process, not only by unit tests: tool use, vision, and cross-protocol same-run behavior.

## Validation Concurrency

### API surface
- Max concurrent validators: **5**
- Rationale:
  - machine capacity observed during dry run: 32 CPUs and ~57 GiB available memory
  - live `giro` process observed at roughly ~21 MiB RSS
  - the validation surface is curl-driven and lightweight relative to machine headroom
  - using 5 keeps a conservative margin while still allowing parallel API assertions

### Startup/auth fixture surface
- Max concurrent validators: **2**
- Rationale:
  - startup/auth validation mutates temp homes, fixture stores, and process startup state
  - lower concurrency reduces cross-test interference when validating precedence/fallback/restart behavior
