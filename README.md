# giro

Use Claude via Kiro — from any tool you already love.

giro is a lightweight API gateway that translates **OpenAI** and **Anthropic** API formats to the [Kiro](https://kiro.dev) API (AWS CodeWhisperer / Amazon Q Developer). Point your favorite AI coding tool at `localhost:8080` and go.

```
┌─────────────────────────────────┐
│  Claude Code · OpenCode · Droid │
│  Cursor · aider · any client    │
└───────────────┬─────────────────┘
                │  OpenAI or Anthropic format
                ▼
          ┌──────────┐
          │   giro   │  translates + authenticates
          └────┬─────┘
               │  Kiro API format
               ▼
         Kiro API (AWS)
```

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/miltonparedes/giro/main/install.sh | sh
```

Detects OS and architecture automatically (Linux & macOS, amd64 & arm64). To install elsewhere:

```sh
curl -fsSL https://raw.githubusercontent.com/miltonparedes/giro/main/install.sh | INSTALL_DIR=~/.local/bin sh
```

**From source:**
```sh
go install github.com/miltonparedes/giro/cmd/giro@latest
```

Or grab a binary from the [releases page](https://github.com/miltonparedes/giro/releases).

## Quick start

```sh
# 1 — Set credentials (pick one method)
export KIRO_CLI_DB_FILE="~/Library/Application Support/kiro-cli/data.sqlite3"  # macOS
export KIRO_CLI_DB_FILE="~/.local/share/kiro-cli/data.sqlite3"                 # Linux
# or: export KIRO_CREDS_FILE="path/to/credentials.json"
# or: export REFRESH_TOKEN="your-token"

# 2 — Start it
just dev          # or: go run ./cmd/giro

# 3 — Check it's alive
curl localhost:8080/health
```

That's it. Now point your tools at `http://localhost:8080`.

## Use with your tools

### Claude Code

```sh
export ANTHROPIC_BASE_URL="http://localhost:8080"
export ANTHROPIC_API_KEY="your-proxy-api-key"  # must match PROXY_API_KEY if set

# If giro rejects anthropic-beta headers:
export CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS=1

claude
```

Or make it permanent in `~/.claude/settings.json`:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:8080",
    "ANTHROPIC_API_KEY": "your-proxy-api-key"
  }
}
```

### OpenCode

Drop an `opencode.json` in your project root (or `~/.config/opencode/opencode.json` for global):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "giro": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "Giro (Kiro Proxy)",
      "options": {
        "baseURL": "http://localhost:8080/v1",
        "apiKey": "{env:PROXY_API_KEY}"
      },
      "models": {
        "claude-sonnet-4": {
          "name": "Claude Sonnet 4",
          "limit": { "context": 200000, "output": 65536 }
        },
        "claude-opus-4-6": {
          "name": "Claude Opus 4.6",
          "limit": { "context": 200000, "output": 32768 }
        }
      }
    }
  },
  "model": "giro/claude-sonnet-4"
}
```

### Droid (Factory)

Add a custom model in `~/.factory/settings.json`:

```json
{
  "customModels": [
    {
      "model": "claude-sonnet-4",
      "displayName": "Giro — Sonnet 4",
      "baseUrl": "http://localhost:8080/v1",
      "apiKey": "${PROXY_API_KEY}",
      "provider": "anthropic"
    },
    {
      "model": "claude-opus-4-6",
      "displayName": "Giro — Opus 4.6",
      "baseUrl": "http://localhost:8080/v1",
      "apiKey": "${PROXY_API_KEY}",
      "provider": "anthropic"
    }
  ]
}
```

Then select it in Droid with `/model` or pass `--model "custom:Giro-0"` in headless mode.

### Cursor

Set the API base URL to `http://localhost:8080/v1` and pick any model from `/v1/models`. If you set `PROXY_API_KEY`, enter that as the API key.

### Any OpenAI-compatible client

```sh
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer your-proxy-api-key" \
  -H "Content-Type: application/json" \
  -d '{"model": "claude-sonnet-4", "messages": [{"role": "user", "content": "Hello!"}]}'
```

### Any Anthropic-compatible client

```sh
curl http://localhost:8080/v1/messages \
  -H "x-api-key: your-proxy-api-key" \
  -H "Content-Type: application/json" \
  -d '{"model": "claude-sonnet-4", "max_tokens": 1024, "messages": [{"role": "user", "content": "Hello!"}]}'
```

## Endpoints

| Method | Path | What it does |
|---|---|---|
| `GET` | `/health` | Health check |
| `GET` | `/v1/models` | List available models (OpenAI format) |
| `POST` | `/v1/chat/completions` | Chat completions (OpenAI format) |
| `POST` | `/v1/messages` | Messages (Anthropic format) |

## Features

- Speaks **both** OpenAI and Anthropic protocols simultaneously
- Streaming (SSE), tool calling, and vision support
- Automatic token refresh — you authenticate once, giro keeps it alive
- Model name normalization — use friendly names like `claude-sonnet-4`

## Authentication

giro handles two auth layers:

1. **Upstream** — giro talks to Kiro using your credentials (refresh token, credentials file, or CLI database)
2. **Client** (optional) — your tools talk to giro using a `PROXY_API_KEY` you define

### Kiro credentials

You need an active [Kiro](https://kiro.dev) session. Pick one:

| Method | How |
|---|---|
| **Kiro CLI database** (easiest) | Set `KIRO_CLI_DB_FILE` to your Kiro SQLite DB path. Tokens refresh automatically. |
| **Credentials file** | Set `KIRO_CREDS_FILE` to a JSON file with `refreshToken`, `accessToken`, `profileArn`, etc. |
| **Direct token** | Set `REFRESH_TOKEN` (and optionally `PROFILE_ARN`). Quick but not persisted across restarts. |

Auth type is detected automatically — if the DB has `clientId` + `clientSecret`, it uses AWS SSO OIDC; otherwise it uses the Kiro Desktop flow.

### Client auth

Off by default. To require it:

```sh
export PROXY_API_KEY="my-secret-key"
```

Clients send this as `Authorization: Bearer <key>` (OpenAI) or `x-api-key: <key>` (Anthropic).

## Configuration

### Core

| Variable | Default | Description |
|---|---|---|
| `HOST` | `0.0.0.0` | Bind address |
| `PORT` | `8080` | Listen port |
| `LOG_LEVEL` | `info` | `debug` · `info` · `warn` · `error` |

### Credentials

| Variable | Description |
|---|---|
| `KIRO_CLI_DB_FILE` | Path to Kiro CLI SQLite database |
| `KIRO_CREDS_FILE` | Path to JSON credentials file |
| `REFRESH_TOKEN` | Kiro refresh token directly |
| `PROFILE_ARN` | AWS CodeWhisperer profile ARN |
| `KIRO_REGION` | AWS region (default: `us-east-1`) |

### Proxy

| Variable | Default | Description |
|---|---|---|
| `PROXY_API_KEY` | _(none)_ | API key clients must provide |
| `VPN_PROXY_URL` | _(none)_ | HTTP proxy for upstream Kiro requests |

### Timeouts & behavior

| Variable | Default | Description |
|---|---|---|
| `STREAMING_READ_TIMEOUT` | `300` | Max seconds waiting for streaming data |
| `FIRST_TOKEN_TIMEOUT` | `15` | Max seconds waiting for first token |
| `FIRST_TOKEN_MAX_RETRIES` | `3` | Retries on first-token timeout |
| `FAKE_REASONING` | `true` | Detect `<thinking>` tags |
| `FAKE_REASONING_HANDLING` | `as_reasoning_content` | `as_reasoning_content` · `remove` · `pass` · `strip_tags` |
| `TRUNCATION_RECOVERY` | `true` | Retry on truncated responses |
| `DEBUG_MODE` | `off` | `off` · `errors` · `all` |

## Building from source

**Requires:** [Go](https://go.dev/) 1.24+ · [just](https://github.com/casey/just) · [gofumpt](https://github.com/mvdan/gofumpt) · [golangci-lint](https://golangci-lint.run/) v2

```sh
just build      # → bin/giro
just test       # tests with race detection
just check      # fmt + lint + test (run before committing)
just cover      # tests with coverage report → coverage.html
```

Run `just` to see all available commands.
