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

### Concrete boot commands
- Explicit-source boot (use when the assertion specifically targets env-backed selection):
  `HOST=127.0.0.1 PORT=8080 PROXY_API_KEY=giro-validation-key KIRO_CLI_DB_FILE=$HOME/.local/share/kiro-cli/data.sqlite3 go run ./cmd/giro`
- Resolved-source boot (use for autodetection/resolved-source assertions on machines with a healthy default local store):
  `HOST=127.0.0.1 PORT=8080 PROXY_API_KEY=giro-validation-key go run ./cmd/giro`

### Validation notes
- `/health` must remain unauthenticated.
- `/v1/models` and `/v1/chat/completions` use OpenAI-style auth/error contracts.
- `/v1/messages` uses Anthropic-style auth/error contracts and accepts `x-api-key` or Bearer.
- Advanced paths must be validated through the live process, not only by unit tests: tool use, vision, and cross-protocol same-run behavior.
- For autodetection-scoped validation, do not force `KIRO_CLI_DB_FILE`; let startup resolve the default store so the evidence reflects real resolved-source behavior.
- Live vision validation should use a non-trivial image fixture (for example a `10x10` PNG or larger); tiny `1x1` images can be rejected upstream as an improperly formed request even when base64 wiring is correct.

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

## Flow Validator Guidance: startup-auth-shell

- Surface: shell-driven startup and credential-resolution validation.
- Port boundary: use only `127.0.0.1:8080`; do not run more than one `giro` process at a time because this milestone's assertions all share the same fixed mission port.
- Isolation boundary:
  - use a fresh temp `HOME` per fixture scenario so autodetected `kiro-cli` / `kiro-ide` paths resolve inside that temp home
  - keep fixture files, logs, and copied stores inside the assigned evidence directory
  - stop `giro` and confirm port `8080` is free before the next startup scenario
- Real-store boundary:
  - read the machine's real `~/.local/share/kiro-cli/data.sqlite3` only for assertions that require live local credentials
  - never print token values, serialized credential blobs, or raw sqlite JSON payloads
- Evidence expectations:
  - capture stdout/stderr for every startup attempt
  - capture the exact curl transcript or status/body excerpt used to prove each assertion
  - when validating rejection/fallback behavior, include both the rejection log and the eventual winning-source log in the report

## Flow Validator Guidance: startup-refresh-shell

- Surface: shell-driven autodetected refresh/persistence validation for `VAL-STARTUP-009`.
- Port boundary: use only `127.0.0.1:8080`; this flow must run alone because it boots, stops, and reboots `giro` on the shared mission port.
- Isolation boundary:
  - create a fresh temp `HOME` under the assigned evidence directory
  - copy the real local `kiro-cli` sqlite store into that temp home so autodetection resolves inside the temp tree
  - modify only the copied fixture store; never mutate the real `~/.local/share/kiro-cli/data.sqlite3` directly
- Runtime expectations:
  - unset explicit auth env vars so startup truly uses autodetection
  - make the copied store look stale-but-refreshable before the first boot
  - capture first boot log, first authenticated request, shutdown, second boot log, and second authenticated request
- Secret handling:
  - logs and reports may mention source/path and token expiry observations
  - never print raw refresh tokens, access tokens, sqlite blobs, or JSON credential payloads

## Flow Validator Guidance: api-shell-curl

- Surface: live HTTP validation against one already-running `giro` process on `http://127.0.0.1:8080`.
- Shared-service boundary:
  - do not start, stop, or restart `giro`; the parent validator owns process lifecycle
  - assume `PROXY_API_KEY=giro-validation-key`
  - keep requests read-only from the service-management perspective; only use `curl`, shell helpers, and files inside the assigned evidence directory
- Isolation boundary:
  - write every request payload, response transcript, and derived helper file under the assigned evidence directory
  - do not reuse another flow validator's temporary files
- Request guidance:
  - prefer `claude-sonnet-4` for direct model requests unless the assertion specifically needs an advertised model from `/v1/models`
  - for tool-use assertions, keep `temperature` at `0` and explicitly require the tool call in the prompt
  - for vision assertions, use a non-trivial local PNG (at least `10x10`); ask for observable image properties such as the dominant color or simple layout
- Evidence expectations:
  - include raw status line / headers for auth and SSE assertions
  - for streaming assertions, save the full SSE transcript and identify the required terminal events
  - for tool-use continuation, save both the first tool-call response and the second `tool_result` continuation response
