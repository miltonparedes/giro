#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

go mod download

for tool in go just golangci-lint gofumpt curl; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "missing required tool: $tool" >&2
    exit 1
  fi
done

if [ ! -f "$HOME/.local/share/kiro-cli/data.sqlite3" ]; then
  echo "warning: default kiro-cli store not found at $HOME/.local/share/kiro-cli/data.sqlite3" >&2
fi
