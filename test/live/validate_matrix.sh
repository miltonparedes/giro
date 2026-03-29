#!/usr/bin/env bash
# validate_matrix.sh — Live validation runner for the giro real matrix.
#
# Boots one fresh giro process on 127.0.0.1:8080, exercises the full validation
# matrix against the real local credential source, and reports pass/fail for
# each item. Designed to be reusable by mission validators and human operators.
#
# Matrix items (VAL-CROSS-001 through VAL-CROSS-005):
#   1. GET /health (unauthenticated)
#   2. GET /v1/models (unauthenticated → 401)
#   3. GET /v1/models (authenticated → 200, auto-kiro + claude-3.7-sonnet)
#   4. POST /v1/chat/completions (OpenAI non-stream)
#   5. POST /v1/chat/completions (OpenAI stream)
#   6. POST /v1/messages (Anthropic non-stream)
#   7. POST /v1/messages (Anthropic stream)
#   8. Negative client auth (OpenAI + Anthropic)
#   9. Tool use (OpenAI)
#  10. Vision (Anthropic base64 image)
#
# Usage:
#   ./test/live/validate_matrix.sh
#
# Environment overrides:
#   GIRO_PROXY_API_KEY  — Client API key (default: giro-validation-key)
#   GIRO_MODEL          — Model for completions (default: claude-sonnet-4)
#   KIRO_CLI_DB_FILE    — Explicit SQLite credential path (default: autodetection)
#   GIRO_BINARY         — Path to prebuilt binary (skips build step)
#   GIRO_SKIP_BUILD     — Set to 1 to skip building (requires GIRO_BINARY)
#   GIRO_MAX_TOKENS     — Max tokens for completions (default: 32)
#   GIRO_TIMEOUT        — Curl timeout in seconds (default: 120)
#   GIRO_STARTUP_WAIT   — Max seconds to wait for health (default: 30)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# --- Configuration ----------------------------------------------------------

API_KEY="${GIRO_PROXY_API_KEY:-giro-validation-key}"
MODEL="${GIRO_MODEL:-claude-sonnet-4}"
MAX_TOKENS="${GIRO_MAX_TOKENS:-32}"
CURL_TIMEOUT="${GIRO_TIMEOUT:-120}"
STARTUP_WAIT="${GIRO_STARTUP_WAIT:-30}"
HOST="127.0.0.1"
PORT="8080"
BASE_URL="http://${HOST}:${PORT}"

# Valid 10×10 solid red PNG (base64). Same fixture used across mock and live
# tests — large enough for real Kiro upstream to accept without rejection.
PNG_BASE64="iVBORw0KGgoAAAANSUhEUgAAAAoAAAAKCAIAAAACUFjqAAAAEklEQVR4nGP4z8CAB+GTG8HSALfKY52fTcuYAAAAAElFTkSuQmCC"

GIRO_PID=""
PASS=0
FAIL=0
RESULTS=()

# --- Helpers ----------------------------------------------------------------

cleanup() {
	if [[ -n "$GIRO_PID" ]] && kill -0 "$GIRO_PID" 2>/dev/null; then
		echo ""
		echo "→ Stopping giro (PID $GIRO_PID)..."
		kill "$GIRO_PID" 2>/dev/null || true
		wait "$GIRO_PID" 2>/dev/null || true
	fi
	# Safety: kill anything left on the port
	lsof -ti :"$PORT" 2>/dev/null | xargs -r kill 2>/dev/null || true
}
trap cleanup EXIT

pass() {
	local name="$1"
	PASS=$((PASS + 1))
	RESULTS+=("  ✓ $name")
	echo "  ✓ PASS: $name"
}

fail() {
	local name="$1"
	shift
	FAIL=$((FAIL + 1))
	RESULTS+=("  ✗ $name — $*")
	echo "  ✗ FAIL: $name — $*"
}

# curl_quiet runs curl and captures status + body. Returns 0 on success.
# Usage: curl_quiet <args...>
# Sets: HTTP_STATUS, HTTP_BODY
curl_quiet() {
	local tmpfile
	tmpfile=$(mktemp)
	HTTP_STATUS=$(curl -s -o "$tmpfile" -w "%{http_code}" --max-time "$CURL_TIMEOUT" "$@" 2>/dev/null) || {
		HTTP_STATUS="000"
		HTTP_BODY=""
		rm -f "$tmpfile"
		return 1
	}
	HTTP_BODY=$(cat "$tmpfile")
	rm -f "$tmpfile"
	return 0
}

# --- Build ------------------------------------------------------------------

build_giro() {
	if [[ -n "${GIRO_BINARY:-}" ]]; then
		echo "→ Using prebuilt binary: $GIRO_BINARY"
		return
	fi
	if [[ "${GIRO_SKIP_BUILD:-}" == "1" ]]; then
		echo "→ Skipping build (GIRO_SKIP_BUILD=1)"
		GIRO_BINARY="$REPO_ROOT/bin/giro"
		return
	fi

	echo "→ Building giro..."
	(cd "$REPO_ROOT" && go build -o bin/giro ./cmd/giro)
	GIRO_BINARY="$REPO_ROOT/bin/giro"
	echo "  Built: $GIRO_BINARY"
}

# --- Start server -----------------------------------------------------------

start_giro() {
	echo "→ Starting giro on ${HOST}:${PORT}..."

	local env_args=(
		"HOST=$HOST"
		"PORT=$PORT"
		"PROXY_API_KEY=$API_KEY"
	)
	# If KIRO_CLI_DB_FILE is set, pass it through for explicit source.
	# Otherwise, let autodetection resolve the credential source.
	if [[ -n "${KIRO_CLI_DB_FILE:-}" ]]; then
		env_args+=("KIRO_CLI_DB_FILE=$KIRO_CLI_DB_FILE")
	fi

	env "${env_args[@]}" "$GIRO_BINARY" &
	GIRO_PID=$!

	echo "  PID: $GIRO_PID"
	echo "→ Waiting for health check (up to ${STARTUP_WAIT}s)..."

	local elapsed=0
	while [[ $elapsed -lt $STARTUP_WAIT ]]; do
		if curl -sf "${BASE_URL}/health" >/dev/null 2>&1; then
			echo "  Server is healthy."
			return
		fi
		# Check process is still alive
		if ! kill -0 "$GIRO_PID" 2>/dev/null; then
			echo "  ✗ giro process exited before becoming healthy."
			exit 1
		fi
		sleep 1
		elapsed=$((elapsed + 1))
	done

	echo "  ✗ Health check timed out after ${STARTUP_WAIT}s."
	exit 1
}

# --- Matrix items -----------------------------------------------------------

# 1. GET /health (unauthenticated)
test_health() {
	local name="health (GET /health, no auth)"
	curl_quiet "${BASE_URL}/health"
	if [[ "$HTTP_STATUS" == "200" ]]; then
		pass "$name"
	else
		fail "$name" "status=$HTTP_STATUS"
	fi
}

# 2. GET /v1/models without auth → 401
test_models_no_auth() {
	local name="models_no_auth (GET /v1/models, no auth → 401)"
	curl_quiet "${BASE_URL}/v1/models"
	if [[ "$HTTP_STATUS" == "401" ]]; then
		pass "$name"
	else
		fail "$name" "status=$HTTP_STATUS, expected 401"
	fi
}

# 3. GET /v1/models with auth → 200 with auto-kiro and claude-3.7-sonnet
test_models_authenticated() {
	local name="models_auth (GET /v1/models, authenticated)"
	curl_quiet -H "Authorization: Bearer $API_KEY" "${BASE_URL}/v1/models"
	if [[ "$HTTP_STATUS" != "200" ]]; then
		fail "$name" "status=$HTTP_STATUS, expected 200"
		return
	fi
	# Check for required model entries
	if echo "$HTTP_BODY" | grep -q '"auto-kiro"' && echo "$HTTP_BODY" | grep -q '"claude-3.7-sonnet"'; then
		pass "$name"
	else
		fail "$name" "missing auto-kiro or claude-3.7-sonnet in model list"
	fi
}

# 4. OpenAI non-stream
test_openai_non_stream() {
	local name="openai_non_stream (POST /v1/chat/completions, stream:false)"
	curl_quiet -X POST "${BASE_URL}/v1/chat/completions" \
		-H "Content-Type: application/json" \
		-H "Authorization: Bearer $API_KEY" \
		-d "{\"model\":\"$MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"Say exactly: ok\"}],\"stream\":false,\"max_tokens\":$MAX_TOKENS}"
	if [[ "$HTTP_STATUS" != "200" ]]; then
		fail "$name" "status=$HTTP_STATUS"
		return
	fi
	if echo "$HTTP_BODY" | grep -q '"choices"'; then
		pass "$name"
	else
		fail "$name" "response missing choices"
	fi
}

# 5. OpenAI stream
test_openai_stream() {
	local name="openai_stream (POST /v1/chat/completions, stream:true)"
	curl_quiet -X POST "${BASE_URL}/v1/chat/completions" \
		-H "Content-Type: application/json" \
		-H "Authorization: Bearer $API_KEY" \
		-d "{\"model\":\"$MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"Say exactly: ok\"}],\"stream\":true,\"max_tokens\":$MAX_TOKENS}"
	if [[ "$HTTP_STATUS" != "200" ]]; then
		fail "$name" "status=$HTTP_STATUS"
		return
	fi
	if echo "$HTTP_BODY" | grep -q 'data: \[DONE\]'; then
		pass "$name"
	else
		fail "$name" "stream missing [DONE] terminator"
	fi
}

# 6. Anthropic non-stream
test_anthropic_non_stream() {
	local name="anthropic_non_stream (POST /v1/messages, stream:false)"
	curl_quiet -X POST "${BASE_URL}/v1/messages" \
		-H "Content-Type: application/json" \
		-H "x-api-key: $API_KEY" \
		-H "anthropic-version: 2023-06-01" \
		-d "{\"model\":\"$MODEL\",\"max_tokens\":$MAX_TOKENS,\"messages\":[{\"role\":\"user\",\"content\":\"Say exactly: ok\"}],\"stream\":false}"
	if [[ "$HTTP_STATUS" != "200" ]]; then
		fail "$name" "status=$HTTP_STATUS"
		return
	fi
	if echo "$HTTP_BODY" | grep -q '"type":"message"'; then
		pass "$name"
	else
		fail "$name" "response missing type:message"
	fi
}

# 7. Anthropic stream
test_anthropic_stream() {
	local name="anthropic_stream (POST /v1/messages, stream:true)"
	curl_quiet -X POST "${BASE_URL}/v1/messages" \
		-H "Content-Type: application/json" \
		-H "x-api-key: $API_KEY" \
		-H "anthropic-version: 2023-06-01" \
		-d "{\"model\":\"$MODEL\",\"max_tokens\":$MAX_TOKENS,\"messages\":[{\"role\":\"user\",\"content\":\"Say exactly: ok\"}],\"stream\":true}"
	if [[ "$HTTP_STATUS" != "200" ]]; then
		fail "$name" "status=$HTTP_STATUS"
		return
	fi
	if echo "$HTTP_BODY" | grep -q 'event: message_stop'; then
		pass "$name"
	else
		fail "$name" "stream missing message_stop event"
	fi
}

# 8. Negative auth — OpenAI
test_negative_auth_openai() {
	local name="negative_auth_openai (invalid Bearer → 401)"
	curl_quiet -H "Authorization: Bearer wrong-key" "${BASE_URL}/v1/models"
	if [[ "$HTTP_STATUS" == "401" ]]; then
		# Verify OpenAI error shape
		if echo "$HTTP_BODY" | grep -q '"error"'; then
			pass "$name"
		else
			fail "$name" "status=401 but missing OpenAI error envelope"
		fi
	else
		fail "$name" "status=$HTTP_STATUS, expected 401"
	fi
}

# 8b. Negative auth — Anthropic
test_negative_auth_anthropic() {
	local name="negative_auth_anthropic (invalid x-api-key → 401)"
	curl_quiet -X POST "${BASE_URL}/v1/messages" \
		-H "Content-Type: application/json" \
		-H "x-api-key: wrong-key" \
		-d '{"model":"claude-sonnet-4","max_tokens":32,"messages":[{"role":"user","content":"hi"}]}'
	if [[ "$HTTP_STATUS" == "401" ]]; then
		# Verify Anthropic error shape
		if echo "$HTTP_BODY" | grep -q '"type":"error"'; then
			pass "$name"
		else
			fail "$name" "status=401 but missing Anthropic error envelope"
		fi
	else
		fail "$name" "status=$HTTP_STATUS, expected 401"
	fi
}

# 9. Tool use (OpenAI)
test_tool_use() {
	local name="tool_use (OpenAI tool calling)"
	curl_quiet -X POST "${BASE_URL}/v1/chat/completions" \
		-H "Content-Type: application/json" \
		-H "Authorization: Bearer $API_KEY" \
		-d '{
			"model":"'"$MODEL"'",
			"messages":[{"role":"user","content":"What is the weather in New York City? You MUST use the get_weather tool to answer."}],
			"stream":false,
			"temperature":0,
			"max_tokens":256,
			"tool_choice":{"type":"function","function":{"name":"get_weather"}},
			"tools":[{
				"type":"function",
				"function":{
					"name":"get_weather",
					"description":"Get current weather for a city",
					"parameters":{
						"type":"object",
						"properties":{"city":{"type":"string","description":"City name"}},
						"required":["city"]
					}
				}
			}]
		}'
	if [[ "$HTTP_STATUS" != "200" ]]; then
		fail "$name" "status=$HTTP_STATUS"
		return
	fi
	if echo "$HTTP_BODY" | grep -q '"tool_calls"'; then
		pass "$name"
	else
		fail "$name" "response missing tool_calls"
	fi
}

# 10. Vision (Anthropic base64 image)
test_vision() {
	local name="vision (Anthropic base64 image)"
	curl_quiet -X POST "${BASE_URL}/v1/messages" \
		-H "Content-Type: application/json" \
		-H "x-api-key: $API_KEY" \
		-H "anthropic-version: 2023-06-01" \
		-d '{
			"model":"'"$MODEL"'",
			"max_tokens":128,
			"messages":[{
				"role":"user",
				"content":[
					{"type":"text","text":"Describe the dominant color of this image in one word."},
					{"type":"image","source":{"type":"base64","media_type":"image/png","data":"'"$PNG_BASE64"'"}}
				]
			}],
			"stream":false
		}'
	if [[ "$HTTP_STATUS" != "200" ]]; then
		fail "$name" "status=$HTTP_STATUS"
		return
	fi
	# The image is a solid red 10×10 PNG. A generic message envelope that
	# ignores the image will not mention "red". Require an image-grounded
	# answer that names the dominant color (case-insensitive).
	if echo "$HTTP_BODY" | grep -qi 'red'; then
		pass "$name"
	else
		fail "$name" "response does not mention 'red' — expected image-grounded answer; body=$HTTP_BODY"
	fi
}

# --- Main -------------------------------------------------------------------

main() {
	echo "═══════════════════════════════════════════════════════════════"
	echo "  giro live validation matrix"
	echo "═══════════════════════════════════════════════════════════════"
	echo ""
	echo "  Model:     $MODEL"
	echo "  Endpoint:  $BASE_URL"
	echo "  Timeout:   ${CURL_TIMEOUT}s per request"
	echo ""

	build_giro
	start_giro

	echo ""
	echo "── Matrix ──────────────────────────────────────────────────────"
	echo ""

	# VAL-CROSS-001: Health public, models authenticated
	test_health
	test_models_no_auth
	test_models_authenticated

	# VAL-CROSS-002 + VAL-CROSS-004: Both protocols, all modes
	test_openai_non_stream
	test_openai_stream
	test_anthropic_non_stream
	test_anthropic_stream

	# VAL-CROSS-003: Negative client auth
	test_negative_auth_openai
	test_negative_auth_anthropic

	# VAL-CROSS-005: Advanced — tool use and vision
	test_tool_use
	test_vision

	echo ""
	echo "── Summary ─────────────────────────────────────────────────────"
	echo ""
	for r in "${RESULTS[@]}"; do
		echo "$r"
	done
	echo ""
	echo "  Total: $((PASS + FAIL))  Passed: $PASS  Failed: $FAIL"
	echo ""

	if [[ $FAIL -gt 0 ]]; then
		echo "  ✗ VALIDATION FAILED"
		exit 1
	else
		echo "  ✓ ALL CHECKS PASSED"
		exit 0
	fi
}

main "$@"
