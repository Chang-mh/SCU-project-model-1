#!/usr/bin/env bash
set -Eeuo pipefail

# Ark / 火山方舟 Embedding end-to-end test for module one.
#
# This wrapper reuses e2e_module_one_full.sh but forces the server-side
# embedding path to Ark/OpenAI-compatible `/embeddings` instead of Ollama.
#
# Prerequisites:
#   1. MySQL is running and the server has been started separately from an
#      environment that includes the Ark embedding variables below.
#   2. Client Python dependencies are installed: cd client && pip install -r requirements.txt
#
# Required environment variables:
#   ARK_API_KEY=your-real-api-key
#   ARK_EMBEDDING_MODEL=your-ark-embedding-endpoint-or-model-id
#
# Optional environment variables:
#   ARK_BASE_URL=https://ark.cn-beijing.volces.com/api/v3
#   E2E_SERVER_URL=http://127.0.0.1:8080
#   E2E_TOKEN=your-server-token
#   E2E_REQUIRE_AGENT=0      # set to 1 only if you also require Ark ChatModel labels
#   E2E_SKIP_UNIT_TESTS=0
#   E2E_REPORT_FILE=MODULE_ONE_ARK_EMBEDDING_E2E_TEST_REPORT.md
#   CLIENT_PYTHON=client/.venv/Scripts/python.exe

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FULL_E2E_SCRIPT="$ROOT_DIR/test/e2e_module_one_full.sh"

fail() {
  printf '[FAIL] %s\n' "$*" >&2
  exit 1
}

has_usable_env() {
  local name="$1"
  local value="${!name:-}"
  [[ -n "$value" && "$value" != "xxx" && "$value" != "xxxxx" ]]
}

has_usable_env ARK_API_KEY || fail "ARK_API_KEY is required for Ark embedding E2E"
has_usable_env ARK_EMBEDDING_MODEL || fail "ARK_EMBEDDING_MODEL is required for Ark embedding E2E"
[[ -f "$FULL_E2E_SCRIPT" ]] || fail "Full E2E script not found: $FULL_E2E_SCRIPT"

export EMBEDDING_PROVIDER="ark"
export OLLAMA_EMBED_MODEL=""
export E2E_REQUIRE_EMBEDDING="1"
export E2E_EXPECT_EMBEDDING_MODEL="$ARK_EMBEDDING_MODEL"
export E2E_EMBEDDING_LABEL="Ark / 火山方舟 Embedding ($ARK_EMBEDDING_MODEL)"
export E2E_REPORT_FILE="${E2E_REPORT_FILE:-$ROOT_DIR/MODULE_ONE_ARK_EMBEDDING_E2E_TEST_REPORT.md}"

printf '[INFO] Running module one E2E with Ark / 火山方舟 Embedding...\n'
printf '[INFO] Server: %s\n' "${E2E_SERVER_URL:-http://127.0.0.1:8080}"
printf '[INFO] Ark base URL: %s\n' "${ARK_BASE_URL:-https://ark.cn-beijing.volces.com/api/v3}"
printf '[INFO] Ark embedding model: %s\n' "$ARK_EMBEDDING_MODEL"
printf '[INFO] Report file: %s\n' "$E2E_REPORT_FILE"
printf '[WARN] Make sure the already-running server was started with EMBEDDING_PROVIDER=ark and the same ARK_EMBEDDING_MODEL.\n'

exec bash "$FULL_E2E_SCRIPT"
