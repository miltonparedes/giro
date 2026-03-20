# giro

Lightweight API gateway that translates OpenAI and Anthropic API formats to the Kiro API (AWS CodeWhisperer / Amazon Q Developer), letting you use Claude models from Kiro with any compatible client — IDEs like Cursor and Cline, SDKs, frameworks like LangChain, or any tool that speaks the OpenAI or Anthropic protocol.

## How it works

```
Client (Cursor, Cline, SDK, etc.)
  │
  │  OpenAI or Anthropic API format
  ▼
┌──────────┐
│   giro   │  ← translates request format
└──────────┘
  │
  │  Kiro API format (authenticated with your Kiro credentials)
  ▼
Kiro API (AWS)
```

giro handles two separate authentication concerns:

1. **Upstream auth** — giro authenticates with the Kiro API using your Kiro credentials (refresh token, credentials file, or CLI database).
2. **Client auth** (optional) — clients authenticate with giro using a proxy API key you define.

## Authentication setup

You need an active Kiro session to provide credentials. giro supports three credential sources (you need at least one):

### Option A: Kiro CLI database (recommended)

If you have the [Kiro IDE](https://kiro.dev) or `kiro-cli` installed, giro can read tokens directly from its SQLite database. This is the easiest method because Kiro manages the tokens for you.

```sh
# Find the database — typically at one of these paths:
# Linux:         ~/.local/share/kiro-cli/data.sqlite3
# macOS (ARM):   ~/Library/Application Support/kiro-cli/data.sqlite3
# macOS (Intel): ~/Library/Application Support/kiro-cli/data.sqlite3

# Linux
export KIRO_CLI_DB_FILE="~/.local/share/kiro-cli/data.sqlite3"
# macOS (both ARM and Intel)
export KIRO_CLI_DB_FILE="~/Library/Application Support/kiro-cli/data.sqlite3"

just dev
```

giro reads the `auth_kv` table looking for token keys (`kirocli:social:token`, `kirocli:odic:token`, or `codewhisperer:odic:token`). It also loads device registration data for AWS SSO OIDC refresh.

**Auth type detected automatically:**
- If the database contains `clientId` + `clientSecret` in a device registration key → **AWS SSO OIDC** flow.
- Otherwise → **Kiro Desktop** flow (uses the Kiro auth refresh endpoint).

### Option B: Credentials JSON file

Point giro at a JSON file containing your tokens:

```sh
export KIRO_CREDS_FILE="~/.config/giro/credentials.json"
just dev
```

The file format:

```json
{
  "refreshToken": "your-refresh-token",
  "accessToken": "your-access-token",
  "profileArn": "arn:aws:codewhisperer:us-east-1:...",
  "region": "us-east-1",
  "expiresAt": "2025-01-01T00:00:00Z"
}
```

For enterprise/SSO setups, include `clientId`, `clientSecret`, or `clientIdHash` (giro will look up the device registration from `~/.aws/sso/cache/<hash>.json`).

giro writes updated tokens back to this file after each refresh.

### Option C: Direct refresh token

Pass a refresh token directly via environment variable:

```sh
export REFRESH_TOKEN="your-kiro-refresh-token"
export PROFILE_ARN="arn:aws:codewhisperer:us-east-1:..."  # optional
just dev
```

This uses the **Kiro Desktop** auth flow. The token refreshes automatically but isn't persisted anywhere — if giro restarts, you need to provide a fresh token.

### How to get a refresh token

1. Log in to [Kiro IDE](https://kiro.dev) or run `kiro-cli login`.
2. After login, the refresh token is stored in Kiro's auth database (Option A) or you can extract it from the IDE's developer tools / local storage.

### Token lifecycle

- Tokens are refreshed automatically **10 minutes before expiry**.
- On a 403 from Kiro, giro force-refreshes the token and retries.
- When using the SQLite source, giro re-reads the database if a refresh fails (in case Kiro IDE refreshed the token independently).
- Refreshed tokens are saved back to the source (SQLite or JSON file).

## Client authentication (optional)

By default, giro accepts all requests without authentication. To require clients to authenticate:

```sh
export PROXY_API_KEY="my-secret-key"
just dev
```

Clients then pass this key in their requests:

```sh
# OpenAI-compatible endpoints — Bearer token
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer my-secret-key" \
  -H "Content-Type: application/json" \
  -d '{"model": "claude-sonnet-4", "messages": [{"role": "user", "content": "Hello"}]}'

# Anthropic-compatible endpoint — x-api-key header or Bearer token
curl http://localhost:8080/v1/messages \
  -H "x-api-key: my-secret-key" \
  -H "Content-Type: application/json" \
  -d '{"model": "claude-sonnet-4", "max_tokens": 1024, "messages": [{"role": "user", "content": "Hello"}]}'

# Opus 4.6 (OpenAI-compatible, streaming)
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer my-secret-key" \
  -H "Content-Type: application/json" \
  -d '{"model": "claude-opus-4-6", "stream": true, "messages": [{"role": "user", "content": "Hello"}]}'
```

## Quick start

```sh
# 1. Set credentials (pick one)
# Linux:
export KIRO_CLI_DB_FILE="~/.local/share/kiro-cli/data.sqlite3"
# macOS:
export KIRO_CLI_DB_FILE="~/Library/Application Support/kiro-cli/data.sqlite3"
# or: export KIRO_CREDS_FILE="path/to/credentials.json"
# or: export REFRESH_TOKEN="your-token"

# 2. Start the server
just dev

# 3. Verify
curl localhost:8080/health
```

### Use with Cursor

Set the API base URL to `http://localhost:8080/v1` and use any model name listed by `/v1/models`. If you set `PROXY_API_KEY`, enter that as the API key in Cursor.

### Use with Cline

Configure the Anthropic API provider with base URL `http://localhost:8080` and use the `/v1/messages` endpoint. If you set `PROXY_API_KEY`, enter that as the API key.

## Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | Health check |
| `GET` | `/v1/models` | List available models (OpenAI compatible) |
| `POST` | `/v1/chat/completions` | Chat completions (OpenAI compatible) |
| `POST` | `/v1/messages` | Messages (Anthropic compatible) |

## Features

- OpenAI and Anthropic API compatibility simultaneously
- Streaming (SSE) support
- Tool calling / function calling
- Vision support
- Automatic token refresh and retry logic
- Model name normalization

## Configuration

### Core

| Variable | Default | Description |
|---|---|---|
| `HOST` | `0.0.0.0` | Bind address |
| `PORT` | `8080` | Listen port |
| `LOG_LEVEL` | `info` | Log level (`debug`, `info`, `warn`, `error`) |

### Credentials (at least one required)

| Variable | Default | Description |
|---|---|---|
| `REFRESH_TOKEN` | | Kiro refresh token (Kiro Desktop auth) |
| `PROFILE_ARN` | | AWS CodeWhisperer profile ARN |
| `KIRO_REGION` | `us-east-1` | AWS region for Kiro API |
| `KIRO_CREDS_FILE` | | Path to JSON credentials file |
| `KIRO_CLI_DB_FILE` | | Path to Kiro CLI SQLite database |

### Proxy

| Variable | Default | Description |
|---|---|---|
| `PROXY_API_KEY` | | API key clients must provide (empty = no auth) |
| `VPN_PROXY_URL` | | HTTP proxy for upstream Kiro requests |

### Timeouts and behavior

| Variable | Default | Description |
|---|---|---|
| `STREAMING_READ_TIMEOUT` | `300` | Max seconds waiting for streaming data |
| `FIRST_TOKEN_TIMEOUT` | `15` | Max seconds waiting for the first token |
| `FIRST_TOKEN_MAX_RETRIES` | `3` | Retries on first-token timeout |
| `FAKE_REASONING` | `true` | Detect and handle `<thinking>` tags |
| `FAKE_REASONING_HANDLING` | `as_reasoning_content` | How to handle thinking tags (`as_reasoning_content`, `remove`, `pass`, `strip_tags`) |
| `TRUNCATION_RECOVERY` | `true` | Retry on truncated responses |
| `DEBUG_MODE` | `off` | Debug logging (`off`, `errors`, `all`) |

## Prerequisites

- [Go](https://go.dev/) 1.24+
- [just](https://github.com/casey/just)
- [gofumpt](https://github.com/mvdan/gofumpt)
- [golangci-lint](https://golangci-lint.run/) v2

## Commands

```sh
just          # List available commands
just dev      # Run in development mode
just build    # Build binary to bin/giro
just test     # Run tests with race detection
just cover    # Run tests with coverage report
just e2e-mock # Run deterministic mock end-to-end tests
just e2e-real # Run real upstream smoke tests (requires env credentials)
just fmt      # Format code
just lint     # Run linter
just lint-fix # Run linter with auto-fix
just check    # Format + lint + test
just tidy     # Tidy and verify dependencies
just clean    # Remove build artifacts
```
