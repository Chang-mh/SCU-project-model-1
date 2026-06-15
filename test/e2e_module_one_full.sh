#!/usr/bin/env bash
set -Eeuo pipefail

# Full end-to-end test for module one: sensitive file identification agent.
#
# Prerequisites:
#   1. MySQL is running and the server has been started separately: cd server && go run .
#   2. Client Python dependencies are installed: cd client && pip install -r requirements.txt
#
# Optional environment variables:
#   E2E_SERVER_URL=http://127.0.0.1:8080
#   E2E_TOKEN=your-server-token
#   E2E_KEEP_TMP=1
#   CLIENT_PYTHON=client/.venv/Scripts/python.exe

SERVER_URL="${E2E_SERVER_URL:-http://127.0.0.1:8080}"
E2E_TOKEN="${E2E_TOKEN:-}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CLIENT_DIR="$ROOT_DIR/client"
RUN_ID="$(date +%Y%m%d%H%M%S)-$$"
# Keep temporary files under the project instead of /tmp so Windows Python can access them
# when this script is launched from Git Bash / MSYS / WSL-like shells.
TMP_ROOT="$ROOT_DIR/.e2e_tmp/scu-model-1-e2e-$RUN_ID"
UPLOAD_DIR="$TMP_ROOT/upload_sample"
SCAN_DIR="$TMP_ROOT/scan_target"
DB_FILE="$TMP_ROOT/sensitive_tags_e2e.db"
HOST_ID="e2e-$RUN_ID"

if [[ -n "${CLIENT_PYTHON:-}" ]]; then
  PYTHON_BIN="$CLIENT_PYTHON"
elif [[ -x "$CLIENT_DIR/.venv/Scripts/python.exe" ]]; then
  PYTHON_BIN="$CLIENT_DIR/.venv/Scripts/python.exe"
elif [[ -x "$CLIENT_DIR/.venv/Scripts/python" ]]; then
  PYTHON_BIN="$CLIENT_DIR/.venv/Scripts/python"
elif [[ -x "$CLIENT_DIR/.venv/bin/python" ]]; then
  PYTHON_BIN="$CLIENT_DIR/.venv/bin/python"
else
  PYTHON_BIN="python"
fi

is_windows_python() {
  [[ "$PYTHON_BIN" == *.exe || "$PYTHON_BIN" == *"/Scripts/python"* ]]
}

py_path() {
  local value="$1"
  if ! is_windows_python; then
    printf '%s' "$value"
    return
  fi
  if command -v cygpath >/dev/null 2>&1; then
    cygpath -w "$value"
    return
  fi
  if [[ "$value" =~ ^/mnt/([A-Za-z])/(.*)$ ]]; then
    local drive="${BASH_REMATCH[1]^^}"
    local rest="${BASH_REMATCH[2]//\//\\}"
    printf '%s:\\%s' "$drive" "$rest"
    return
  fi
  if [[ "$value" =~ ^/([A-Za-z])/(.*)$ ]]; then
    local drive="${BASH_REMATCH[1]^^}"
    local rest="${BASH_REMATCH[2]//\//\\}"
    printf '%s:\\%s' "$drive" "$rest"
    return
  fi
  printf '%s' "$value"
}

AUTH_ARGS=()
if [[ -n "$E2E_TOKEN" && "$E2E_TOKEN" != "change-me" ]]; then
  AUTH_ARGS=(-H "Authorization: Bearer $E2E_TOKEN")
fi

pass_count=0
fail_count=0

log() {
  printf '\n[INFO] %s\n' "$*"
}

pass() {
  pass_count=$((pass_count + 1))
  printf '[PASS] %s\n' "$*"
}

fail() {
  fail_count=$((fail_count + 1))
  printf '[FAIL] %s\n' "$*" >&2
  exit 1
}

on_error() {
  local line="$1"
  printf '\n[ERROR] Script failed near line %s. Temporary directory: %s\n' "$line" "$TMP_ROOT" >&2
}
trap 'on_error $LINENO' ERR

cleanup() {
  if [[ "${E2E_KEEP_TMP:-0}" == "1" ]]; then
    printf '\n[INFO] E2E_KEEP_TMP=1, keeping temporary directory: %s\n' "$TMP_ROOT"
  else
    rm -rf "$TMP_ROOT"
  fi
}
trap cleanup EXIT

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "Required command not found: $1"
}

curl_get() {
  local url="$1"
  local out_file="$2"
  curl -sS -o "$out_file" -w "%{http_code}" "${AUTH_ARGS[@]}" "$url" || true
}

curl_post_json() {
  local url="$1"
  local json_file="$2"
  local out_file="$3"
  curl -sS -o "$out_file" -w "%{http_code}" \
    "${AUTH_ARGS[@]}" \
    -H "Content-Type: application/json" \
    -d "@$json_file" \
    "$url" || true
}

probe_server_status() {
  local base_url="$1"
  curl -sS -o /dev/null -w "%{http_code}" "${AUTH_ARGS[@]}" "$base_url/api/client/rules?version=0" 2>/dev/null || true
}

auto_fix_wsl_server_url() {
  local status port candidate candidate_status host_ip
  status="$(probe_server_status "$SERVER_URL")"
  if [[ "$status" != "000" ]]; then
    return
  fi
  if [[ "$SERVER_URL" != http://127.0.0.1:* && "$SERVER_URL" != http://localhost:* ]]; then
    return
  fi
  port="$(printf '%s' "$SERVER_URL" | sed -E 's#^http://(127\.0\.0\.1|localhost):([0-9]+).*$#\2#')"
  if [[ -z "$port" || "$port" == "$SERVER_URL" ]]; then
    return
  fi

  # In WSL, Windows services are often reachable through the default gateway,
  # sometimes through resolv.conf nameserver, and in some setups through
  # host.docker.internal. Try all common host aliases before failing.
  local candidates=()
  if command -v ip >/dev/null 2>&1; then
    host_ip="$(ip route show default 2>/dev/null | awk '{print $3; exit}')"
    [[ -n "$host_ip" ]] && candidates+=("$host_ip")
  fi
  if [[ -r /etc/resolv.conf ]]; then
    host_ip="$(awk '/^nameserver / {print $2; exit}' /etc/resolv.conf)"
    [[ -n "$host_ip" ]] && candidates+=("$host_ip")
  fi
  candidates+=("host.docker.internal")

  local seen=""
  for host_ip in "${candidates[@]}"; do
    [[ -z "$host_ip" ]] && continue
    if [[ "$seen" == *"|$host_ip|"* ]]; then
      continue
    fi
    seen="$seen|$host_ip|"
    candidate="http://$host_ip:$port"
    candidate_status="$(probe_server_status "$candidate")"
    if [[ "$candidate_status" != "000" ]]; then
      printf '[INFO] %s is unreachable from this Bash environment; using host URL %s instead.\n' "$SERVER_URL" "$candidate"
      SERVER_URL="$candidate"
      return
    fi
  done

  printf '[WARN] %s is unreachable from this Bash environment. Tried host candidates: %s\n' "$SERVER_URL" "$seen" >&2
  printf '[WARN] If PowerShell curl works but bash curl fails, run with E2E_SERVER_URL=http://<Windows-host-ip>:%s\n' "$port" >&2
}

assert_http_status() {
  local actual="$1"
  local expected="$2"
  local label="$3"
  local body_file="${4:-}"
  if [[ "$actual" != "$expected" ]]; then
    if [[ -n "$body_file" && -f "$body_file" ]]; then
      printf '\n[DEBUG] Response body for %s:\n' "$label" >&2
      cat "$body_file" >&2 || true
      printf '\n' >&2
    fi
    fail "$label expected HTTP $expected, got $actual"
  fi
  pass "$label returned HTTP $expected"
}

json_assert() {
  local json_file="$1"
  local label="$2"
  local python_code="$3"
  local py_json_file
  py_json_file="$(py_path "$json_file")"
  "$PYTHON_BIN" - "$py_json_file" "$label" <<PY
import json
import sys

path = sys.argv[1]
label = sys.argv[2]
with open(path, 'r', encoding='utf-8') as f:
    data = json.load(f)

$python_code
PY
  pass "$label"
}

run_client() {
  local client_script py_db
  client_script="$(py_path "$CLIENT_DIR/client.py")"
  py_db="$(py_path "$DB_FILE")"
  local converted_args=()
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --path)
        converted_args+=("$1" "$(py_path "$2")")
        shift 2
        ;;
      *)
        converted_args+=("$1")
        shift
        ;;
    esac
  done
  SERVER_API_TOKEN="${E2E_TOKEN:-change-me}" "$PYTHON_BIN" "$client_script" --db "$py_db" "${converted_args[@]}"
}

require_cmd curl
require_cmd "$PYTHON_BIN"
auto_fix_wsl_server_url

log "Using server: $SERVER_URL"
log "Using Python: $PYTHON_BIN"
log "Temporary directory: $TMP_ROOT"

mkdir -p "$UPLOAD_DIR" "$SCAN_DIR/nested"

log "Checking Python client dependencies"
"$PYTHON_BIN" - <<'PY'
missing = []
for name in ["requests", "loguru", "chardet", "jieba", "openpyxl", "pypdf", "docx"]:
    try:
        __import__(name)
    except Exception:
        missing.append(name)
if missing:
    raise SystemExit("Missing Python dependencies: " + ", ".join(missing) + ". Run: cd client && pip install -r requirements.txt")
PY
pass "Python client dependencies are importable"

SAMPLE_FILE="$UPLOAD_DIR/customer_${RUN_ID}.txt"
cat > "$SAMPLE_FILE" <<SAMPLE
客户名称：四川示例科技有限公司
联系人：张三
手机号：13800138000
邮箱：test@example.com
报价：50万元
合同金额：50万元
API_KEY = abcdefghijklmnop123456
测试批次：$RUN_ID
SAMPLE

cp "$SAMPLE_FILE" "$SCAN_DIR/customer_copy.txt"
cat > "$SCAN_DIR/customer_modified.txt" <<SAMPLE
客户名称：四川示例科技有限公司
联系人：李四
手机号：13900139000
邮箱：demo@example.com
报价：80万元
合同金额：80万元
这是未公开客户报价资料，测试批次：$RUN_ID。
SAMPLE

cat > "$SCAN_DIR/secret_config.py" <<'SAMPLE'
API_KEY = "abcdefghijklmnop1234567890"
password = "SuperSecret123"
db = "jdbc:mysql://127.0.0.1:3306/app"
private_ip = "192.168.1.10"
SAMPLE

cat > "$SCAN_DIR/normal.txt" <<'SAMPLE'
这是一份普通项目说明文档。
内容只描述今天的天气、会议时间和公开资料整理要求。
SAMPLE

cat > "$SCAN_DIR/nested/nested_customer.txt" <<SAMPLE
客户名称：成都测试客户有限公司
联系人：王五
手机号：13700137000
报价：66万元
合同金额：66万元
测试批次：$RUN_ID
SAMPLE

ZIP_FILE_PY="$(py_path "$SCAN_DIR/sensitive.zip")"
"$PYTHON_BIN" - "$ZIP_FILE_PY" <<'PY'
import sys
import zipfile
zip_path = sys.argv[1]
with zipfile.ZipFile(zip_path, 'w', compression=zipfile.ZIP_DEFLATED) as zf:
    zf.writestr('zip_customer.txt', '客户名称：ZIP客户有限公司\n联系人：赵六\n手机号：13600136000\n报价：30万元\n合同金额：30万元\n')
PY
pass "Prepared E2E sample files"

log "TC01: server rules endpoint connectivity"
health_json="$TMP_ROOT/health.json"
status="$(curl_get "$SERVER_URL/api/client/rules?version=0" "$health_json")"
assert_http_status "$status" "200" "GET /api/client/rules health" "$health_json"
json_assert "$health_json" "rules endpoint response has required fields" '
for key in ["latest_version", "rules", "fingerprints", "config"]:
    if key not in data:
        raise SystemExit(f"missing key: {key}")
'

log "TC02: upload sensitive sample"
upload_json="$TMP_ROOT/upload.json"
status="$(curl -sS -o "$upload_json" -w "%{http_code}" \
  "${AUTH_ARGS[@]}" \
  -F "file=@$SAMPLE_FILE" \
  -F "sensitive_type=客户资料" \
  -F "risk_level=high" \
  -F "description=客户报价和联系人信息 E2E $RUN_ID" \
  "$SERVER_URL/api/server/samples" || true)"
assert_http_status "$status" "200" "POST /api/server/samples" "$upload_json"
json_assert "$upload_json" "upload response contains rules, version and fingerprints" '
if not data.get("sensitive_file_id"):
    raise SystemExit("missing sensitive_file_id")
if int(data.get("rule_version", 0)) < 1:
    raise SystemExit("rule_version should be >= 1")
if int(data.get("generated_rules_count", 0)) <= 0:
    raise SystemExit("generated_rules_count should be > 0")
fingerprint = data.get("fingerprint") or {}
if not fingerprint.get("sha256") or not fingerprint.get("simhash"):
    raise SystemExit("missing fingerprint sha256/simhash")
rule_types = {r.get("rule_type") or r.get("type") for r in data.get("generated_rules", [])}
for expected in ["regex", "keyword", "combined"]:
    if expected not in rule_types:
        raise SystemExit(f"missing generated rule type: {expected}; got {sorted(rule_types)}")
'

UPLOAD_JSON_PY="$(py_path "$upload_json")"
SAMPLE_SHA256="$($PYTHON_BIN - "$UPLOAD_JSON_PY" <<'PY' | tr -d '\r\n'
import json, sys
with open(sys.argv[1], encoding='utf-8') as f:
    data = json.load(f)
print((data.get('fingerprint') or {}).get('sha256', ''))
PY
)"

log "TC03: duplicate upload should be rejected"
duplicate_json="$TMP_ROOT/duplicate.json"
status="$(curl -sS -o "$duplicate_json" -w "%{http_code}" \
  "${AUTH_ARGS[@]}" \
  -F "file=@$SAMPLE_FILE" \
  -F "sensitive_type=客户资料" \
  -F "risk_level=high" \
  -F "description=duplicate E2E $RUN_ID" \
  "$SERVER_URL/api/server/samples" || true)"
assert_http_status "$status" "409" "duplicate POST /api/server/samples" "$duplicate_json"
json_assert "$duplicate_json" "duplicate response contains duplicate details" '
if "duplicates" not in data or not data["duplicates"]:
    raise SystemExit("expected non-empty duplicates")
'

log "TC04: query uploaded sensitive file by sha256"
query_sensitive_json="$TMP_ROOT/sensitive_file_query.json"
status="$(curl_get "$SERVER_URL/api/server/sensitive-files/$SAMPLE_SHA256" "$query_sensitive_json")"
assert_http_status "$status" "200" "GET /api/server/sensitive-files/{sha256}" "$query_sensitive_json"
json_assert "$query_sensitive_json" "sensitive file query returns uploaded sample" '
if not (data.get("sensitive_file_id") or data.get("id")):
    raise SystemExit("missing sensitive_file_id/id")
'

log "TC05: rules sync endpoint contains generated and builtin rules"
rules_json="$TMP_ROOT/rules_after_upload.json"
status="$(curl_get "$SERVER_URL/api/client/rules?version=0" "$rules_json")"
assert_http_status "$status" "200" "GET /api/client/rules after upload" "$rules_json"
json_assert "$rules_json" "rules sync response contains executable rules and config" '
if int(data.get("latest_version", 0)) < 1:
    raise SystemExit("latest_version should be >= 1")
rules = data.get("rules") or []
fingerprints = data.get("fingerprints") or []
if not rules:
    raise SystemExit("rules should not be empty")
if not fingerprints:
    raise SystemExit("fingerprints should not be empty")
rule_types = {r.get("rule_type") for r in rules}
for expected in ["regex", "keyword", "combined"]:
    if expected not in rule_types:
        raise SystemExit(f"missing rule type from sync: {expected}; got {sorted(rule_types)}")
config = data.get("config") or {}
if "simhash_threshold" not in config:
    raise SystemExit("missing config.simhash_threshold")
if "semantic_label_hints" not in config:
    raise SystemExit("missing config.semantic_label_hints")
'

log "TC06: client sync writes local SQLite cache"
sync_json="$TMP_ROOT/client_sync.json"
run_client sync --server "$SERVER_URL" > "$sync_json"
json_assert "$sync_json" "client sync command succeeds" '
if data.get("success") is not True:
    raise SystemExit(f"sync failed: {data}")
if int(data.get("version", 0)) < 1:
    raise SystemExit("synced version should be >= 1")
'
[[ -f "$DB_FILE" ]] || fail "SQLite DB was not created: $DB_FILE"
pass "SQLite DB file created"

DB_FILE_PY="$(py_path "$DB_FILE")"
"$PYTHON_BIN" - "$DB_FILE_PY" <<'PY'
import sqlite3
import sys
conn = sqlite3.connect(sys.argv[1])
cur = conn.cursor()
checks = {
    'cached_rules': 'SELECT COUNT(*) FROM cached_rules',
    'cached_fingerprints': 'SELECT COUNT(*) FROM cached_fingerprints',
    'local_rules_version': 'SELECT COALESCE(MAX(version), 0) FROM local_rules_version',
}
values = {}
for name, sql in checks.items():
    cur.execute(sql)
    values[name] = cur.fetchone()[0]
if values['cached_rules'] <= 0:
    raise SystemExit(f"cached_rules should be > 0: {values}")
if values['cached_fingerprints'] <= 0:
    raise SystemExit(f"cached_fingerprints should be > 0: {values}")
if values['local_rules_version'] < 1:
    raise SystemExit(f"local_rules_version should be >= 1: {values}")
PY
pass "SQLite rule cache contains rules, fingerprints and version"

log "TC07: client scans directory and returns JSON results"
scan_json="$TMP_ROOT/scan_results.json"
run_client scan --path "$SCAN_DIR" --server "$SERVER_URL" --json > "$scan_json"
json_assert "$scan_json" "scan output is non-empty JSON array" '
if not isinstance(data, list) or not data:
    raise SystemExit("scan output should be a non-empty array")
'

json_assert "$scan_json" "exact copy hits SHA-256 with score 100" '
rows = data
row = next((r for r in rows if str(r.get("file_path", "")).endswith("customer_copy.txt")), None)
if row is None:
    raise SystemExit("customer_copy.txt result missing")
if row.get("sensitive") is not True:
    raise SystemExit(f"customer_copy should be sensitive: {row}")
if row.get("confidence_level") != "sensitive":
    raise SystemExit(f"customer_copy confidence should be sensitive: {row}")
if int(row.get("match_score", 0)) != 100:
    raise SystemExit(f"customer_copy score should be 100: {row}")
if not (row.get("match_detail") or {}).get("sha256_hit"):
    raise SystemExit(f"customer_copy should have sha256_hit: {row}")
'

json_assert "$scan_json" "modified customer file is recognized as non-clean" '
rows = data
row = next((r for r in rows if str(r.get("file_path", "")).endswith("customer_modified.txt")), None)
if row is None:
    raise SystemExit("customer_modified.txt result missing")
if int(row.get("match_score", 0)) < 30:
    raise SystemExit(f"customer_modified score should be >= 30: {row}")
if row.get("confidence_level") == "clean":
    raise SystemExit(f"customer_modified should not be clean: {row}")
'

json_assert "$scan_json" "secret config file is recognized by regex rules" '
rows = data
row = next((r for r in rows if str(r.get("file_path", "")).endswith("secret_config.py")), None)
if row is None:
    raise SystemExit("secret_config.py result missing")
if int(row.get("match_score", 0)) < 30:
    raise SystemExit(f"secret_config score should be >= 30: {row}")
if row.get("confidence_level") == "clean":
    raise SystemExit(f"secret_config should not be clean: {row}")
regex_hits = (row.get("match_detail") or {}).get("regex_hits") or []
if not regex_hits:
    raise SystemExit(f"secret_config should have regex_hits: {row}")
'

json_assert "$scan_json" "normal file stays clean" '
rows = data
row = next((r for r in rows if str(r.get("file_path", "")).endswith("normal.txt")), None)
if row is None:
    raise SystemExit("normal.txt result missing")
if row.get("sensitive") is not False:
    raise SystemExit(f"normal file should not be sensitive: {row}")
if row.get("confidence_level") != "clean":
    raise SystemExit(f"normal file should be clean: {row}")
if int(row.get("match_score", 0)) >= 30:
    raise SystemExit(f"normal file score should be < 30: {row}")
'

json_assert "$scan_json" "nested directory file is scanned" '
rows = data
row = next((r for r in rows if str(r.get("file_path", "")).endswith("nested_customer.txt")), None)
if row is None:
    raise SystemExit("nested_customer.txt result missing")
if int(row.get("match_score", 0)) < 30:
    raise SystemExit(f"nested_customer score should be >= 30: {row}")
'

json_assert "$scan_json" "zip inner file is scanned" '
rows = data
row = next((r for r in rows if "sensitive.zip!" in str(r.get("file_path", "")) and str(r.get("file_path", "")).endswith("zip_customer.txt")), None)
if row is None:
    raise SystemExit("ZIP inner zip_customer.txt result missing")
if int(row.get("match_score", 0)) < 30:
    raise SystemExit(f"zip_customer score should be >= 30: {row}")
'

log "TC08: SQLite local tags and list command"
TAG_COUNT_BEFORE="$($PYTHON_BIN - "$DB_FILE_PY" <<'PY' | tr -d '\r\n'
import sqlite3, sys
conn = sqlite3.connect(sys.argv[1])
cur = conn.cursor()
cur.execute('SELECT COUNT(*) FROM local_file_tags')
print(cur.fetchone()[0])
PY
)"
if [[ "$TAG_COUNT_BEFORE" -le 0 ]]; then
  fail "local_file_tags should contain scanned rows"
fi
pass "SQLite local_file_tags contains $TAG_COUNT_BEFORE rows"

list_json="$TMP_ROOT/list_sensitive.json"
run_client list --sensitive-only --json > "$list_json"
json_assert "$list_json" "client list --sensitive-only returns sensitive rows" '
if not isinstance(data, list) or not data:
    raise SystemExit("sensitive-only list should not be empty")
if not any(r.get("sensitive") in (1, True) and int(r.get("match_score") or 0) >= 80 for r in data):
    raise SystemExit(f"expected at least one high-score sensitive row: {data}")
'

log "TC09: repeated scan updates existing tags without duplicate growth"
run_client scan --path "$SCAN_DIR" --server "$SERVER_URL" --json > "$TMP_ROOT/scan_results_repeat.json"
TAG_COUNT_AFTER="$($PYTHON_BIN - "$DB_FILE_PY" <<'PY' | tr -d '\r\n'
import sqlite3, sys
conn = sqlite3.connect(sys.argv[1])
cur = conn.cursor()
cur.execute('SELECT COUNT(*) FROM local_file_tags')
print(cur.fetchone()[0])
PY
)"
if [[ "$TAG_COUNT_AFTER" != "$TAG_COUNT_BEFORE" ]]; then
  fail "Repeated scan should not increase local_file_tags row count: before=$TAG_COUNT_BEFORE after=$TAG_COUNT_AFTER"
fi
pass "Repeated scan preserved local_file_tags row count ($TAG_COUNT_AFTER)"

log "TC10: report scan results to server"
report_payload="$TMP_ROOT/report_payload.json"
REPORT_SCAN_JSON_PY="$(py_path "$scan_json")"
REPORT_PAYLOAD_PY="$(py_path "$report_payload")"
SCAN_DIR_PY="$(py_path "$SCAN_DIR")"
"$PYTHON_BIN" - "$REPORT_SCAN_JSON_PY" "$REPORT_PAYLOAD_PY" "$HOST_ID" "$SCAN_DIR_PY" <<'PY'
import json
import sys
from datetime import datetime
scan_json, out_json, host_id, scan_path = sys.argv[1:5]
with open(scan_json, encoding='utf-8') as f:
    results = json.load(f)
payload = {
    'host_id': host_id,
    'scan_path': scan_path,
    'scanned_at': datetime.now().isoformat(timespec='seconds'),
    'results': results,
}
with open(out_json, 'w', encoding='utf-8') as f:
    json.dump(payload, f, ensure_ascii=False)
PY
report_json="$TMP_ROOT/report_response.json"
status="$(curl_post_json "$SERVER_URL/api/client/scan-results" "$report_payload" "$report_json")"
assert_http_status "$status" "200" "POST /api/client/scan-results" "$report_json"
json_assert "$report_json" "scan report response accepted" '
if data.get("status") != "received":
    raise SystemExit(f"expected status=received: {data}")
if not data.get("report_id"):
    raise SystemExit("missing report_id")
if int(data.get("received", -1)) <= 0:
    raise SystemExit(f"received should be > 0: {data}")
'

log "TC11: query reported scan results from server"
reported_json="$TMP_ROOT/reported_results.json"
status="$(curl_get "$SERVER_URL/api/server/scan-results?host_id=$HOST_ID&sensitive_only=1&limit=20" "$reported_json")"
assert_http_status "$status" "200" "GET /api/server/scan-results" "$reported_json"
json_assert "$reported_json" "reported scan results are queryable" '
if int(data.get("total", 0)) <= 0:
    raise SystemExit(f"expected total > 0: {data}")
if not data.get("results"):
    raise SystemExit(f"expected non-empty results: {data}")
'

log "TC12: nonexistent scan path fails"
if run_client scan --path "$TMP_ROOT/does-not-exist" --server "$SERVER_URL" --no-sync >/dev/null 2>&1; then
  fail "Scanning a nonexistent path should fail"
else
  pass "Scanning a nonexistent path fails as expected"
fi

printf '\n========================================\n'
printf 'Module One Full E2E PASSED (%d checks)\n' "$pass_count"
printf 'Temporary directory: %s\n' "$TMP_ROOT"
printf '========================================\n'
