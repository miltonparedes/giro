# Architecture

This document captures the high-level architecture that should remain true while evolving `giro`. It belongs here when a change affects component boundaries, request/auth data flow, startup behavior, or the validation surface that protects compatibility.

## Components

### Startup composition
- `cmd/giro/main.go` is the composition root.
- It loads runtime config, validates startup prerequisites, creates the shared HTTP client, creates the auth manager, warms the model cache, builds the resolver, and wires the router/server.
- This layer should stay thin: it decides **what components exist**, not **how credentials are discovered or how protocols are translated**.

### Explicit config vs autodetection
- `internal/config` is the source of explicit operator intent: host/port, proxy key, timeouts, region, and any manually supplied credential pointers or tokens.
- Today startup is driven by explicit env-backed credential inputs (`REFRESH_TOKEN`, `KIRO_CREDS_FILE`, `KIRO_CLI_DB_FILE`).
- For this mission, autodetection should become a **separate resolution layer**, not extra branching scattered through handlers or config parsing.
- Target shape:
  - **Config** = explicit overrides and runtime knobs.
  - **Credential resolution** = chooses the winning source from explicit config first, then safe autodetection.
  - **Auth manager** = consumes one resolved source and manages token lifecycle from there.
 - Important implication: the current fail-closed startup gate in `internal/config` / `cmd/giro` assumes explicit credential env vars exist. This mission changes that gate from “an explicit env credential must exist” to “at least one credential source must resolve.”

### Credential source resolution
- The auth subsystem currently knows how to load credentials from:
  - a raw refresh token,
  - a JSON credentials file (`kiro-ide` style),
  - a SQLite database (`kiro-cli` style).
- The mission should preserve that loading capability but introduce a clearer boundary:
  - discovery/probing of candidate sources,
  - precedence and fallback decisions,
  - normalization into one resolved source object with metadata such as `source`, `path`, auth type hints, and persistence mode.
- This keeps platform-default path detection, future store formats, and precedence policy outside the token-refresh core.

### Credential resolution contract
- Treat each candidate source as one of these classes:
  - `env-refresh-token`
  - `env-creds-file`
  - `env-sqlite`
  - autodetected `kiro-cli`
  - autodetected `kiro-ide`
- Resolution policy for this mission:
  1. explicit env-backed sources first,
  2. autodetected `kiro-cli`,
  3. autodetected `kiro-ide`.
- Combined explicit-source precedence must remain backward compatible:
  - `KIRO_CLI_DB_FILE` beats `KIRO_CREDS_FILE`,
  - file-backed sources beat bare `REFRESH_TOKEN`.
- Each candidate should move through explicit states:
  - missing
  - present
  - unreadable
  - unparsable
  - usable
  - selected
  - rejected
- Startup logs should make the final decision observable:
  - winning `source`
  - resolved `path` when file-backed
  - rejected higher-priority candidates and why they were skipped
- “Safe fallback” means a bad higher-priority candidate does not silently win, and a valid lower-priority candidate can still start the server when policy allows fallback.

### Auth manager
- `internal/auth` owns upstream Kiro authentication once a source is selected.
- Responsibilities:
  - load initial auth material,
  - detect whether the flow is Kiro Desktop or AWS SSO OIDC,
  - refresh access tokens,
  - persist refreshed credentials back to the same backing store when applicable,
  - expose API/Q hosts and profile metadata to the rest of the system.
- It should remain the only component that talks to refresh endpoints or mutates persisted credentials.

### Source persistence matrix
- `env-refresh-token`
  - writable: no
  - persistence: none; refreshed values are process-local only
  - note: this source exists for explicit override/escape hatch behavior
- `env-creds-file`
  - writable: yes
  - persistence target: the same credentials file
  - invariant: preserve the file as the active source of truth instead of jumping to another store
- `env-sqlite`
  - writable: yes
  - persistence target: the same SQLite DB and matching auth key family
  - invariant: refresh writes back to the same selected DB source
- autodetected `kiro-cli`
  - writable: yes, same behavior as today
  - persistence target: the same detected SQLite DB path
- autodetected `kiro-ide`
  - writable: yes for the selected credentials file
  - companion registration material may also be required for enterprise-style auth
  - invariant: detect and use companion registration material without leaking secrets, while keeping the selected file path/source stable in logs
- Reload-before-refresh behavior is part of the auth layer contract for writable file-backed sources when stale-on-disk state can become fresher than in-memory state.

### HTTP server and middleware
- `internal/server` builds a chi router with shared middleware.
- Health endpoints remain public.
- OpenAI-compatible endpoints use OpenAI-style auth middleware and error envelopes.
- Anthropic-compatible endpoints use Anthropic-style auth middleware and error envelopes.
- Middleware should keep enforcing the local client-facing contract; upstream Kiro auth remains separate.

### Protocol compatibility table
| Route | Client auth contract | Response/error contract |
| --- | --- | --- |
| `/` and `/health` | public | local JSON health payload |
| `/v1/models` | OpenAI Bearer auth when `PROXY_API_KEY` is enabled | OpenAI model-list shape and OpenAI auth errors |
| `/v1/chat/completions` | OpenAI Bearer auth when `PROXY_API_KEY` is enabled | OpenAI JSON/SSE shape and OpenAI error envelope |
| `/v1/messages` | Anthropic `x-api-key` or Bearer when `PROXY_API_KEY` is enabled | Anthropic JSON/SSE shape and Anthropic error envelope |

- Auth/startup refactors must not change:
  - route set
  - header semantics
  - local auth failure shapes
  - shared support for stream and non-stream responses
  - tool-use and vision behavior

### Protocol handlers
- `internal/handler/openai.go` and `internal/handler/anthropic.go` are protocol-edge adapters.
- Their role is to:
  - parse client requests,
  - resolve the requested model name,
  - invoke the conversion pipeline,
  - send one upstream request through the shared auth/client stack,
  - format the result back into the caller’s protocol.
- They should not own credential discovery logic or protocol-agnostic token logic.

### Conversion and stream pipeline
- `internal/convert` maps OpenAI and Anthropic requests into one Kiro-oriented payload shape.
- `internal/kiro` sends authenticated upstream requests with retry/refresh behavior.
- `internal/stream` parses the Kiro stream into protocol-agnostic events, then formats those events back into OpenAI SSE/JSON or Anthropic SSE/JSON.
- Architectural intent:
  - **conversion** handles request-shape normalization,
  - **kiro client** handles transport and retry,
  - **stream** handles response normalization.

### Model cache and resolver
- Startup fetches available Kiro models into `internal/model` cache, with configured fallback models when upstream listing is unavailable.
- The resolver applies a stable pipeline: aliasing, normalization, dynamic cache lookup, hidden-model mapping, then passthrough.
- `/v1/models` is served from this resolver/cache boundary, not by directly proxying upstream model listing on each request.

## Data Flows

### 1. Startup and credential selection
1. Load explicit env config.
2. Resolve credentials:
   - explicit sources first,
   - autodetected `kiro-cli` / `kiro-ide` sources next,
   - deterministic fallback when a higher-priority candidate is invalid.
3. Construct the auth manager from the single winning source.
4. Warm the model cache using that auth context.
5. Start one HTTP server that serves both OpenAI and Anthropic surfaces.

### 2. OpenAI / Anthropic request flow
1. Client request hits protocol-specific middleware.
2. Handler parses protocol-specific JSON.
3. Model resolver translates the external model name into the Kiro-facing model choice.
4. Conversion layer builds the Kiro payload from protocol messages/tools/images.
5. Kiro client gets/refreshes an access token and sends the upstream request.
6. Stream parser converts the Kiro response into protocol-agnostic events.
7. Response formatter emits protocol-correct JSON or SSE back to the client.

### 3. Auth refresh and persistence flow
1. Auth manager serves cached access tokens when still valid.
2. If the token is stale, it reloads from the selected store when appropriate, then refreshes if needed.
3. On successful refresh, it persists updated credentials back to the same resolved source when that source is writable.
4. Request handlers remain unaware of whether auth came from env, file, SQLite, or autodetection.

### 4. Model advertisement flow
1. Startup fetches model metadata from Kiro when possible.
2. Hidden models and aliases are layered onto the cache.
3. `/v1/models` exposes the client-facing model list from the resolver’s merged view.
4. Request-time model resolution uses the same resolver so advertised names and accepted names stay aligned.

## Invariants

- `giro` is one process with one shared upstream auth context serving both OpenAI and Anthropic APIs.
- Health endpoints stay unauthenticated even when client API-key protection is enabled elsewhere.
- Explicit operator configuration must have higher priority than autodetection.
- Multiple explicit sources must keep backward-compatible precedence.
- Credential discovery and precedence decisions must be deterministic and observable.
- The auth manager must receive one resolved source, not re-run ad hoc precedence logic during requests.
- Refresh persistence must write back only to the source that supplied the active credentials; refresh must not silently jump stores.
- Handlers translate protocols; they do not own token storage, token refresh, or source discovery.
- The conversion/stream pipeline is shared across protocols so behavior changes apply consistently to OpenAI and Anthropic surfaces.
- Model resolution must remain tolerant: unknown model names pass through instead of failing early unless a deliberate contract change says otherwise.
- Logs and validation evidence may include source type, path, region, and auth mode, but must never expose refresh tokens, access tokens, client secrets, or raw credential blobs.

## Extension Points

### Credential discovery
- Add platform-aware probes for default `kiro-cli` and `kiro-ide` locations behind a dedicated resolver/discovery interface.
- New sources should plug in as additional candidates without changing handler code or refresh code.

### Resolution policy
- Represent resolution as a first-class result: winning source, rejected candidates, precedence reason, resolved path, and persistence behavior.
- This is the right place for explicit-vs-autodetected precedence, safe fallback, and structured startup logging.

### Auth backends
- The auth manager can continue supporting multiple refresh styles (Kiro Desktop, AWS SSO OIDC), but backend selection should be based on resolved source material rather than on hardcoded startup branches elsewhere.

### Protocol surfaces
- OpenAI and Anthropic handlers can evolve independently at the edge as long as they continue to feed the shared conversion/request/stream pipeline.

### Validation surface
- Startup behavior, credential precedence, fallback, refresh persistence, local auth errors, model listing, request translation, streaming, tool use, and vision are all architectural contracts, not incidental implementation details.

## Validation-Relevant Notes

- The highest-risk regression area for this mission is startup credential resolution, because current startup assumes explicit credential env vars while the target behavior adds autodetection.
- Validation is layered:
  - resolver/auth unit tests for precedence, fallback, parsing, and persistence policy,
  - handler/server integration tests for protocol contracts and auth/error shapes,
  - live local validation for autodetected real stores and both protocol families in one process.
- Validation must cover:
  - fail-closed startup when no source resolves,
  - explicit source precedence over autodetection,
  - backward-compatible precedence among multiple explicit env sources,
  - deterministic ordering between autodetected `kiro-cli` and `kiro-ide`,
  - safe fallback when a higher-priority source is broken,
  - enterprise-style `kiro-ide` discovery with companion registration material,
  - refresh + persistence for autodetected stores,
  - secret-safe logs and errors.
- API compatibility must remain intact after the startup/auth changes:
  - OpenAI `/v1/models` and `/v1/chat/completions`,
  - Anthropic `/v1/messages`,
  - streaming and non-streaming flows,
  - tool-use and vision paths,
  - protocol-correct local auth failures.
- Cross-surface validation matters because one resolved source must support both protocol families in the same process lifetime.
- This mission is not allowed to rely on mocks alone for the end state: the contract requires live startup behavior and local real-flow validation using the machine’s real credential source.
