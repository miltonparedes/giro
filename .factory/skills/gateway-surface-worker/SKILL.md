---
name: gateway-surface-worker
description: Implement and verify OpenAI/Anthropic surface compatibility, advanced paths, and local validation assets for giro.
---

# Gateway Surface Worker

NOTE: Startup and cleanup are handled by `worker-base`. This skill defines the work procedure.

## When to Use This Skill

Use this skill for features that touch OpenAI or Anthropic handlers, conversion/streaming behavior, model advertisement, tool use, vision, local validation harnesses, or repeatable real-flow validation.

## Required Skills

None.

## Work Procedure

1. Read `mission.md`, `validation-contract.md`, `AGENTS.md`, `.factory/library/architecture.md`, and `.factory/library/user-testing.md` before editing code.
2. Translate the feature's `fulfills` list into failing tests first.
   - Prefer handler/integration tests for protocol contracts.
   - Add e2e or local-validation assets when the feature is about same-run or real-flow coverage.
   - Keep stream assertions explicit about framing and terminal events.
   - If the feature is primarily assurance/coverage for behavior that already works, record that as a justified exception in the handoff instead of claiming pure red→green TDD compliance.
3. Implement the smallest change that preserves or restores the client-facing protocol contract.
   - Do not move credential discovery into handlers.
   - Preserve local auth/error shapes for OpenAI and Anthropic separately.
   - Keep tool use, stream, and vision paths compatible with the shared conversion pipeline.
4. Run focused package tests first, then the broader repository validators.
5. For any feature that changes observable API behavior, boot one local process on `127.0.0.1:8080` and validate with `curl`.
   - Capture at least one real request per affected surface.
   - If the feature touches cross-area validation, run the affected subset in one process lifetime.
6. Stop the local process and confirm no orphan listeners remain on port `8080`.
7. In the handoff, report exact routes exercised, whether tool/vision behavior was real and local, and any gaps that still need another feature.

## Example Handoff

```json
{
  "salientSummary": "Preserved the OpenAI and Anthropic route contracts after the auth refactor, restored image-grounded vision on both protocols, and added repeatable local validation coverage for the same-run matrix.",
  "whatWasImplemented": "Updated the shared conversion path and affected handlers so OpenAI and Anthropic non-stream, stream, tool use, and vision requests still complete under the resolved-source startup flow, then added local validation assets that exercise both protocol families against one live process.",
  "whatWasLeftUndone": "",
  "verification": {
    "commandsRun": [
      {
        "command": "go test -race -count=1 ./internal/handler ./internal/stream ./internal/convert ./test/e2e",
        "exitCode": 0,
        "observation": "Route, conversion, stream, and e2e coverage passed for the touched surfaces."
      },
      {
        "command": "go test -race -count=1 -p 16 ./...",
        "exitCode": 0,
        "observation": "Full repository test suite passed after the surface changes."
      },
      {
        "command": "golangci-lint run",
        "exitCode": 0,
        "observation": "No lint regressions."
      }
    ],
    "interactiveChecks": [
      {
        "action": "Started giro on 127.0.0.1:8080 and ran OpenAI non-stream, OpenAI stream, Anthropic non-stream, Anthropic stream, tool use, and vision curls against the same process.",
        "observed": "All requests succeeded with protocol-correct JSON/SSE framing; negative auth still returned local 401s; both vision probes produced image-grounded answers."
      }
    ]
  },
  "tests": {
    "added": [
      {
        "file": "test/e2e/real_e2e_test.go",
        "cases": [
          {
            "name": "TestRealOpenAIStreamAndToolUse",
            "verifies": "The OpenAI surface preserves SSE framing and tool-call behavior against the live local process."
          },
          {
            "name": "TestRealAnthropicVision",
            "verifies": "Anthropic base64 image inputs stay grounded through the live local path."
          }
        ]
      }
    ]
  },
  "discoveredIssues": []
}
```

## When to Return to Orchestrator

- The feature would require changing an approved external route, auth header contract, or error envelope.
- Real local validation is blocked by unavailable credentials, upstream outage, or a port-boundary change.
- The needed fix spans both protocol surfaces and startup/auth architecture in a way that should be decomposed further.
- The route behavior appears to depend on undocumented product intent rather than observable repo/contract evidence.
