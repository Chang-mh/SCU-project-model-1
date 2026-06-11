#!/usr/bin/env bash
set -euo pipefail

SERVER_URL="${E2E_SERVER_URL:-http://127.0.0.1:8080}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CLIENT_DIR="$ROOT_DIR/client"
TMP_DIR="${TMPDIR:-/tmp}/scu-model-1-e2e"
SAMPLE_FILE="$TMP_DIR/customer.txt"
DB_FILE="$TMP_DIR/sensitive_tags_e2e.db"

if ! command -v curl >/dev/null 2>&1; then
  echo "SKIP: curl not found"
  exit 0
fi

if ! curl -fsS "$SERVER_URL/api/client/rules?version=0" >/dev/null 2>&1; then
  echo "SKIP: server is not reachable at $SERVER_URL"
  echo "Set E2E_SERVER_URL if the server runs elsewhere."
  exit 0
fi

mkdir -p "$TMP_DIR"
cat > "$SAMPLE_FILE" <<'SAMPLE'
客户名称：四川示例科技有限公司
联系人：张三
手机号：13800138000
邮箱：test@example.com
报价：50万元
SAMPLE

curl -fsS \
  -F "file=@$SAMPLE_FILE" \
  -F "sensitive_type=客户资料" \
  -F "risk_level=high" \
  -F "description=客户报价和联系人信息" \
  "$SERVER_URL/api/server/samples" >/dev/null

(
  cd "$CLIENT_DIR"
  python client.py --db "$DB_FILE" sync --server "$SERVER_URL" >/dev/null
  result="$(python client.py --db "$DB_FILE" scan --path "$SAMPLE_FILE" --server "$SERVER_URL" --json)"
  python - <<'PY' "$result"
import json
import sys
rows = json.loads(sys.argv[1])
if not rows:
    raise SystemExit("scan returned no rows")
row = rows[0]
if not row.get("sensitive"):
    raise SystemExit(f"expected sensitive=true, got {row}")
if row.get("confidence_level") != "sensitive":
    raise SystemExit(f"expected confidence_level=sensitive, got {row}")
if row.get("match_score", 0) < 80:
    raise SystemExit(f"expected score >= 80, got {row}")
print("E2E OK")
PY
)
