# Environment

Environment variables, external dependencies, and setup notes.

**What belongs here:** required env vars, external API/service dependencies, credential-source assumptions, platform notes.
**What does NOT belong here:** service ports/commands (use `.factory/services.yaml`).

---

## Runtime assumptions

- Validation for this mission uses the machine's real Kiro/AWS access.
- The default local real credential source on this machine is the `kiro-cli` SQLite store at `~/.local/share/kiro-cli/data.sqlite3`.
- `kiro-ide` autodetection is part of mission scope and must be testable through fixtures and/or real local stores when available.

## Relevant env vars

- `HOST`
- `PORT`
- `LOG_LEVEL`
- `PROXY_API_KEY`
- `REFRESH_TOKEN`
- `PROFILE_ARN`
- `KIRO_CREDS_FILE`
- `KIRO_CLI_DB_FILE`
- `KIRO_REGION`
- `VPN_PROXY_URL`
- streaming and behavior flags already supported by the repo (`FIRST_TOKEN_TIMEOUT`, `FIRST_TOKEN_MAX_RETRIES`, `FAKE_REASONING`, `FAKE_REASONING_HANDLING`, `TRUNCATION_RECOVERY`, `DEBUG_MODE`)

## Mission-specific guidance

- Explicit env-backed credential sources keep highest precedence.
- Autodetection must stay secret-safe: logs may show source/path, never tokens or raw blobs.
- Refresh persistence for writable selected sources remains enabled because the user asked to keep the current behavior.
