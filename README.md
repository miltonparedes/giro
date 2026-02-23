# giro

Lightweight API gateway that translates OpenAI and Anthropic API formats to the Kiro API (AWS CodeWhisperer / Amazon Q Developer), letting you use Claude models from Kiro with any compatible client — IDEs like Cursor and Cline, SDKs, frameworks like LangChain, or any tool that speaks the OpenAI or Anthropic protocol.


### Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | Health check |
| `GET` | `/v1/models` | List available models (OpenAI compatible) |
| `POST` | `/v1/chat/completions` | Chat completions (OpenAI compatible) |
| `POST` | `/v1/messages` | Messages (Anthropic compatible) |

### Features

- OpenAI and Anthropic API compatibility simultaneously
- Streaming (SSE) support
- Tool calling / function calling
- Vision support
- Automatic token refresh and retry logic
- Model name normalization

## Prerequisites

- [Go](https://go.dev/) 1.24+
- [just](https://github.com/casey/just)
- [gofumpt](https://github.com/mvdan/gofumpt)
- [golangci-lint](https://golangci-lint.run/) v2

## Quick start

```sh
just dev
```

The server starts on `http://localhost:8080`. Verify with:

```sh
curl localhost:8080/health
```

## Configuration

| Variable | Default | Description |
|---|---|---|
| `HOST` | `0.0.0.0` | Bind address |
| `PORT` | `8080` | Listen port |
| `LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `API_KEY` | | Kiro API key |

## Commands

```sh
just          # List available commands
just dev      # Run in development mode
just build    # Build binary to bin/giro
just test     # Run tests with race detection
just cover    # Run tests with coverage report
just fmt      # Format code
just lint     # Run linter
just lint-fix # Run linter with auto-fix
just check    # Format + lint + test
just tidy     # Tidy and verify dependencies
just clean    # Remove build artifacts
```
