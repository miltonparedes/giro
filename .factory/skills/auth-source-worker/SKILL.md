---
name: auth-source-worker
description: Implement startup credential-resolution, autodetection, and auth persistence features for giro.
---

# Auth Source Worker

NOTE: Startup and cleanup are handled by `worker-base`. This skill defines the work procedure.

## When to Use This Skill

Use this skill for features that touch config validation, startup wiring, credential discovery, source precedence, autodetection, auth persistence, or selected-source logging.

## Required Skills

None.

## Work Procedure

1. Read `mission.md`, `validation-contract.md`, `AGENTS.md`, `.factory/library/architecture.md`, and `.factory/library/environment.md` before changing code.
2. From the feature's `fulfills` list, identify the startup/auth behaviors you must make testable. Write failing tests first and observe them fail before implementation changes.
   - Prefer table-driven unit tests for candidate resolution and precedence.
   - Use temp dirs / temp HOME layouts for autodetection fixtures.
   - Add narrow startup/integration tests when behavior depends on process boot or logging.
   - If a feature truly cannot start with a failing test first, say why in the handoff instead of claiming full procedure adherence.
3. Implement the smallest change that introduces or updates the credential-resolution boundary.
   - Keep handlers and protocol formatters free of discovery logic.
   - Preserve explicit-source precedence and source-specific persistence behavior.
   - Keep logs secret-safe while still exposing source/path metadata required by the contract.
4. Run focused auth/startup tests first, then broaden to repository-level validators.
5. If the feature changes real startup behavior, run one local smoke against `127.0.0.1:8080` using the service manifest or the mission-approved command.
   - Record the exact startup command and the exact `/health` verification command in the handoff.
   - Capture the winning source/path and confirm secrets are not printed.
6. Stop any process you started. Never leave `giro` or test helpers running.
7. In the handoff, be explicit about which sources were tested, which precedence/fallback cases were exercised, and whether persistence hit a writable store.

## Example Handoff

```json
{
  "salientSummary": "Introduced a resolved-source layer for startup, added kiro-cli autodetection with explicit-source precedence, and verified local startup still serves traffic without leaking secrets.",
  "whatWasImplemented": "Added credential candidate resolution and selected-source metadata, updated startup to gate on a resolved source instead of env-only validation, and added temp-home coverage for explicit precedence, autodetected kiro-cli selection, and fail-closed startup.",
  "whatWasLeftUndone": "",
  "verification": {
    "commandsRun": [
      {
        "command": "go test -race -count=1 ./internal/auth ./internal/config ./cmd/giro",
        "exitCode": 0,
        "observation": "Resolver and startup tests passed, including explicit precedence and no-source failure cases."
      },
      {
        "command": "go test -race -count=1 -p 16 ./...",
        "exitCode": 0,
        "observation": "Full repository test suite passed after the auth/startup refactor."
      },
      {
        "command": "golangci-lint run",
        "exitCode": 0,
        "observation": "No lint regressions."
      },
      {
        "command": "curl -sf http://127.0.0.1:8080/health",
        "exitCode": 0,
        "observation": "Local startup smoke succeeded after booting giro with the selected source."
      }
    ],
    "interactiveChecks": [
      {
        "action": "Started giro on 127.0.0.1:8080 with the local kiro-cli SQLite store and requested /health.",
        "observed": "Startup logged source=kiro-cli with the resolved path, /health returned 200, and no secret values appeared in logs."
      }
    ]
  },
  "tests": {
    "added": [
      {
        "file": "internal/auth/resolver_test.go",
        "cases": [
          {
            "name": "TestResolvePrefersExplicitSourceOverAutodetection",
            "verifies": "Explicit env-backed sources remain higher priority than autodetected stores."
          },
          {
            "name": "TestResolveFallsBackFromBrokenExplicitSource",
            "verifies": "Invalid explicit sources are rejected and the next viable candidate is selected."
          }
        ]
      }
    ]
  },
  "discoveredIssues": []
}
```

## When to Return to Orchestrator

- A required default probe location or source format cannot be inferred safely from repo evidence.
- The feature requires changing the approved persistence policy.
- Real local credential stores are unavailable or too risky to exercise for the needed validation.
- A startup/auth change would require altering protocol contracts outside the feature scope.
