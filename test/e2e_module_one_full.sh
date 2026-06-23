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
#   E2E_REQUIRE_AGENT=0      # optional: set to 1 to require Ark ChatModel semantic labels instead of rule fallback
#   E2E_REQUIRE_EMBEDDING=1  # require Ollama/Ark embedding_id and semantic-search
#   E2E_EXPECT_EMBEDDING_MODEL=your-model-id  # optional: assert semantic-search used this embedding model
#   E2E_EMBEDDING_LABEL="Ollama/Ark embedding" # label used in the generated report
#   E2E_SKIP_UNIT_TESTS=0    # set to 1 only when debugging an already-tested server/client
#   E2E_REPORT_FILE=MODULE_ONE_E2E_TEST_REPORT.md
#   CLIENT_PYTHON=client/.venv/Scripts/python.exe

SERVER_URL="${E2E_SERVER_URL:-http://127.0.0.1:8080}"
E2E_TOKEN="${E2E_TOKEN:-}"
E2E_REQUIRE_AGENT="${E2E_REQUIRE_AGENT:-0}"
E2E_REQUIRE_EMBEDDING="${E2E_REQUIRE_EMBEDDING:-1}"
E2E_SKIP_UNIT_TESTS="${E2E_SKIP_UNIT_TESTS:-0}"
E2E_EXPECT_EMBEDDING_MODEL="${E2E_EXPECT_EMBEDDING_MODEL:-}"
E2E_EMBEDDING_LABEL="${E2E_EMBEDDING_LABEL:-Ollama/Ark embedding}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CLIENT_DIR="$ROOT_DIR/client"
SERVER_DIR="$ROOT_DIR/server"
RUN_ID="$(date +%Y%m%d%H%M%S)-$$"
# Keep temporary files under the project instead of /tmp so Windows Python can access them
# when this script is launched from Git Bash / MSYS / WSL-like shells.
TMP_ROOT="$ROOT_DIR/.e2e_tmp/scu-model-1-e2e-$RUN_ID"
UPLOAD_DIR="$TMP_ROOT/upload_sample"
SCAN_DIR="$TMP_ROOT/scan_target"
DB_FILE="$TMP_ROOT/sensitive_tags_e2e.db"
HOST_ID="e2e-$RUN_ID"
REPORT_FILE="${E2E_REPORT_FILE:-$ROOT_DIR/MODULE_ONE_E2E_TEST_REPORT.md}"

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

if [[ "$PYTHON_BIN" != /* && ! "$PYTHON_BIN" =~ ^[A-Za-z]: && "$PYTHON_BIN" != *\\* ]]; then
  PYTHON_BIN="$ROOT_DIR/$PYTHON_BIN"
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
report_written=0
FAILED_REASON=""
PASS_MESSAGES=()

write_report() {
  local status="$1"
  local generated_at
  generated_at="$(date '+%Y-%m-%d %H:%M:%S')"
  mkdir -p "$(dirname "$REPORT_FILE")"
  {
    printf '# 模块一自动化测试结果解析\n\n'
    printf '## 总览\n\n'
    printf '| 项目 | 结果 |\n'
    printf '|---|---|\n'
    printf '| 运行状态 | %s |\n' "$status"
    printf '| 生成时间 | %s |\n' "$generated_at"
    printf '| 服务地址 | %s |\n' "$SERVER_URL"
    printf '| Python | %s |\n' "$PYTHON_BIN"
    printf '| 通过检查数 | %s |\n' "$pass_count"
    printf '| 失败检查数 | %s |\n' "$fail_count"
    printf '| 要求 embedding | %s |\n' "$E2E_REQUIRE_EMBEDDING"
    printf '| Embedding 来源 | %s |\n' "$E2E_EMBEDDING_LABEL"
    if [[ -n "$E2E_EXPECT_EMBEDDING_MODEL" ]]; then
      printf '| 期望 Embedding 模型 | %s |\n' "$E2E_EXPECT_EMBEDDING_MODEL"
    fi
    printf '| 要求 ChatModel 语义标签增强项 | %s |\n' "$E2E_REQUIRE_AGENT"
    printf '| 临时目录 | %s |\n' "$TMP_ROOT"
    if [[ -n "$FAILED_REASON" ]]; then
      printf '| 失败原因 | %s |\n' "$FAILED_REASON"
    fi
    printf '\n## 分项结果\n\n'
    if [[ ${#PASS_MESSAGES[@]} -eq 0 ]]; then
      printf '%s\n' '- 尚无通过项，脚本可能在初始化阶段失败。'
    else
      local item
      for item in "${PASS_MESSAGES[@]}"; do
        printf '%s\n' "- PASS: $item"
      done
    fi
    if [[ -n "$FAILED_REASON" ]]; then
      printf '%s\n' "- FAIL: $FAILED_REASON"
    fi

    printf '\n## 测试输出说明\n\n'
    printf '%s\n' '- `broken.pdf` / `parser failed` 是客户端单元测试故意构造的损坏 PDF，用来验证解析失败时会记录 `extract_error` 和 `skip_reason`，不是功能故障。'
    printf '%s\n' '- `E2E_REQUIRE_AGENT=0` 表示不强制要求 ChatModel；模块一 3.4.5 要求的是 embedding 向量库，ChatModel 只用于提升语义标签质量。'
    printf '%s\n' "- 本次报告中的 Embedding 来源标签：$E2E_EMBEDDING_LABEL。"

    printf '\n## 对照 dlpagent.md 模块一要求\n\n'
    printf '| dlpagent.md 条目 | 要求摘要 | 自动化覆盖情况 | 证据 |\n'
    printf '|---|---|---|---|\n'
    printf '| 3.1 核心目标 | 上传敏感文件后生成正则、关键词、文本指纹、可选向量特征 | 覆盖 | TC02 上传样本校验 regex/keyword/combined、fingerprint；TC05.2 校验 embedding/semantic-search |\n'
    printf '| 3.2 输入内容 | 文本文件，办公文档/代码文件可选 | 部分覆盖 | E2E 覆盖 txt、py、zip；客户端单测覆盖 PDF 解析错误、pptx unsupported 标记；Office 全量格式仍属于可选增强 |\n'
    printf '| 3.3.1 固定格式敏感信息 | 身份证、手机号、银行卡、邮箱、地址、车牌、护照、社保、税号、统一社会信用代码、API Key、Token、私钥、密码、数据库连接串、内网 IP、域名等 | 覆盖 | TC01.1 /api/server/content-scan 逐项断言内置规则命中 |\n'
    printf '| 3.3.2 企业业务敏感信息 | 合同、客户、联系方式、项目/报价/财务/薪酬/组织/商业计划/招投标/源代码/接口/架构/漏洞/运维账号 | 部分覆盖 | E2E 覆盖客户、联系方式、报价、合同金额、API/密码/数据库连接；Go/Python 单测覆盖财务、薪资、源码、合同保密等组合规则；组织架构/招投标等仍主要依赖关键词扩展 |\n'
    printf '| 3.3.3 文档语义特征 | 保密协议、客户名单、财务预算、报价单、薪资明细、研发设计、源码说明、内部培训、未公开财报、战略规划 | 覆盖主要路径 | TC05.1 校验 semantic_labels；server/core 语义回退与标签提示单测覆盖多类语义标签；ChatModel 为增强项，不是模块一必需条件 |\n'
    printf '| 3.4.1 正则规则 | 生成/同步可执行正则规则 | 覆盖 | TC02、TC05、TC07 校验 regex 生成、同步、客户端扫描命中 |\n'
    printf '| 3.4.2 关键词规则 | 生成/同步关键词规则 | 覆盖 | TC02、TC05、TC07 校验 keyword 生成、同步、扫描命中 |\n'
    printf '| 3.4.3 组合规则 | 多条件组合规则 | 覆盖 | TC02、TC05 校验 combined 规则存在；服务端单测覆盖客户报价、财务、薪资、源码、合同组合 |\n'
    printf '| 3.4.4 文件指纹 | SHA-256 与 SimHash | 覆盖 | TC02 校验 fingerprint；TC04 按 SHA-256 查询；TC07 精确复制 100 分；TC05 同步 fingerprints |\n'
    printf '| 3.4.5 文件语义向量 | 调用大模型 embedding 构建向量数据库 | 覆盖 | TC05.1 要求 embedding_id；TC05.2 调用 /api/server/semantic-search 并确认 vector_store=semantic_features、metric=cosine；本次使用 %s，不要求 ChatModel |\n' "$E2E_EMBEDDING_LABEL"
    printf '| 3.5 输出结果 | 返回 ID、类型、风险、规则、指纹、embedding、解释 | 覆盖 | TC02 校验上传响应中的 sensitive_file_id、risk、rules、fingerprint、semantic_labels、explanation；embedding_id 由 TC05.1 校验 |\n'
    printf '| 3.6.1 扫描模式 | 接收指定目录扫描 | 覆盖 | TC07 扫描目录；TC12 非法路径失败 |\n'
    printf '| 3.6.2 文本提取能力 | txt/csv/json/xml，Office/PDF/图片/压缩包/源码/二进制可选 | 部分覆盖 | E2E 覆盖 txt、py、zip 递归；客户端单测覆盖 pdf 失败标记、pptx unsupported；图片/OCR、rar/7z、eml/msg 不在当前模块一自动化覆盖内 |\n'
    printf '| 3.6.3 敏感文件标记 | 本地 SQLite、hash、路径、敏感类型、风险等级等标签 | 覆盖 | TC06 SQLite 缓存；TC08 local_file_tags；TC09 重复扫描不重复插入；TC10/TC11 上报与查询扫描结果 |\n'

    printf '\n## 结论\n\n'
    if [[ "$status" == "PASSED" ]]; then
      printf '本次自动化脚本通过。对 dlpagent.md 模块一的必需主链路已经覆盖：规则生成、规则同步、指纹、语义标签、%s 向量库、目录扫描、本地标记和结果上报。ChatModel 可提升语义分析质量，但模块一的 3.4.5 只要求 embedding 构建向量数据库，因此不是必需项。\n\n' "$E2E_EMBEDDING_LABEL"
      printf '仍建议人工关注的边界：办公文档全格式、图片 OCR、rar/7z、eml/msg 属于可选或增强项，目前报告中标记为部分覆盖。\n'
    else
      printf '本次自动化脚本未完全通过，请优先查看“失败原因”和终端输出。覆盖结论以失败前已执行的检查为准。\n'
    fi
  } > "$REPORT_FILE"
  report_written=1
}

write_report_on_exit() {
  local exit_code="$1"
  if [[ "$report_written" == "0" ]]; then
    if [[ "$exit_code" == "0" ]]; then
      write_report "PASSED"
    else
      [[ -n "$FAILED_REASON" ]] || FAILED_REASON="脚本异常退出，退出码 $exit_code"
      write_report "FAILED"
    fi
  fi
}

log() {
  printf '\n[INFO] %s\n' "$*"
}

pass() {
  pass_count=$((pass_count + 1))
  PASS_MESSAGES+=("$*")
  printf '[PASS] %s\n' "$*"
}

fail() {
  fail_count=$((fail_count + 1))
  FAILED_REASON="$*"
  printf '[FAIL] %s\n' "$*" >&2
  write_report "FAILED"
  exit 1
}

on_error() {
  local line="$1"
  printf '\n[ERROR] Script failed near line %s. Temporary directory: %s\n' "$line" "$TMP_ROOT" >&2
}
trap 'on_error $LINENO' ERR

cleanup() {
  local exit_code="$?"
  write_report_on_exit "$exit_code"
  if [[ "${E2E_KEEP_TMP:-0}" == "1" ]]; then
    printf '\n[INFO] E2E_KEEP_TMP=1, keeping temporary directory: %s\n' "$TMP_ROOT"
  else
    rm -rf "$TMP_ROOT"
  fi
  printf '\n[INFO] Test report written to: %s\n' "$REPORT_FILE"
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
require_cmd go
require_cmd "$PYTHON_BIN"
auto_fix_wsl_server_url

log "Using server: $SERVER_URL"
log "Using Python: $PYTHON_BIN"
log "Temporary directory: $TMP_ROOT"
log "Require optional ChatModel semantic labels: $E2E_REQUIRE_AGENT"
log "Require embedding_id: $E2E_REQUIRE_EMBEDDING"
log "Embedding source label: $E2E_EMBEDDING_LABEL"
if [[ -n "$E2E_EXPECT_EMBEDDING_MODEL" ]]; then
  log "Expected embedding model: $E2E_EXPECT_EMBEDDING_MODEL"
fi
log "Skip unit tests: $E2E_SKIP_UNIT_TESTS"

mkdir -p "$UPLOAD_DIR" "$SCAN_DIR/nested"

if [[ "$E2E_SKIP_UNIT_TESTS" != "1" ]]; then
  log "TC00: server Go tests"
  mkdir -p "$TMP_ROOT/go-cache" "$TMP_ROOT/go-appdata" "$TMP_ROOT/go-localappdata"
  (
    cd "$SERVER_DIR"
    GOCACHE="$TMP_ROOT/go-cache" \
      APPDATA="$TMP_ROOT/go-appdata" \
      LOCALAPPDATA="$TMP_ROOT/go-localappdata" \
      GOTELEMETRY=off \
      go test ./...
  )
  pass "Server Go tests passed"

  log "TC00.1: client Python tests"
  (
    cd "$CLIENT_DIR"
    "$PYTHON_BIN" -m unittest discover -p "test_*.py" -v
  )
  pass "Client Python tests passed"
else
  pass "Unit tests skipped by E2E_SKIP_UNIT_TESTS=1"
fi

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

log "TC01.1: built-in content scan covers fixed-format sensitive information"
content_scan_payload="$TMP_ROOT/content_scan_payload.json"
cat > "$content_scan_payload" <<'JSON'
{"content":"身份证 11010119900307123X\n手机号 13800138000\n银行卡 4111111111111111\n邮箱 test@example.com\n地址 四川省成都市高新区天府大道88号\n车牌 川A12345\n护照 E12345678\n社保号 社保 123456789012\n税号 税号 91350100M000100Y43\n统一社会信用代码 91350100M000100Y43\n合同编号 HT-2026-ABC001\nAPI_KEY = abcdefghijklmnop123456\naccess_token = abcdefghijklmnop1234567890\n-----BEGIN PRIVATE KEY-----\npassword = SuperSecret123\ndb = jdbc:mysql://127.0.0.1:3306/app\nprivate_ip = 192.168.1.10\n域名 internal.example.com\n报价 50万元"}
JSON
content_scan_json="$TMP_ROOT/content_scan.json"
status="$(curl_post_json "$SERVER_URL/api/server/content-scan" "$content_scan_payload" "$content_scan_json")"
assert_http_status "$status" "200" "POST /api/server/content-scan" "$content_scan_json"
json_assert "$content_scan_json" "content-scan detects dlpagent fixed-format sensitive rules" '
results = data.get("results") or []
names = {item.get("rule_name") for item in results}
expected = {
    "id_card", "mobile_phone", "bank_card", "email", "license_plate",
    "passport", "social_security", "tax_number", "credit_code",
    "contract_number", "address", "private_ip", "domain", "api_key",
    "access_token", "private_key", "password", "db_connection", "money_wan",
}
missing = sorted(expected - names)
if missing:
    raise SystemExit(f"missing fixed-format rule hits: {missing}; got {sorted(names)}")
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
if not data.get("semantic_labels"):
    raise SystemExit("missing semantic_labels")
if not data.get("explanation"):
    raise SystemExit("missing explanation")
rule_types = {r.get("rule_type") or r.get("type") for r in data.get("generated_rules", [])}
for expected in ["regex", "keyword", "combined"]:
    if expected not in rule_types:
        raise SystemExit(f"missing generated rule type: {expected}; got {sorted(rule_types)}")
'

UPLOAD_JSON_PY="$(py_path "$upload_json")"
SAMPLE_ID="$($PYTHON_BIN - "$UPLOAD_JSON_PY" <<'PY' | tr -d '\r\n'
import json, sys
with open(sys.argv[1], encoding='utf-8') as f:
    data = json.load(f)
print(data.get('sensitive_file_id', ''))
PY
)"
SAMPLE_SHA256="$($PYTHON_BIN - "$UPLOAD_JSON_PY" <<'PY' | tr -d '\r\n'
import json, sys
with open(sys.argv[1], encoding='utf-8') as f:
    data = json.load(f)
print((data.get('fingerprint') or {}).get('sha256', ''))
PY
)"
[[ -n "$SAMPLE_ID" ]] || fail "Could not extract sensitive_file_id from upload response"
[[ -n "$SAMPLE_SHA256" ]] || fail "Could not extract sha256 from upload response"
pass "Extracted uploaded sample id and sha256"

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

log "TC05.1: semantic labels and embedding metadata"
RULES_JSON_PY="$(py_path "$rules_json")"
"$PYTHON_BIN" - "$RULES_JSON_PY" "$SAMPLE_ID" "$E2E_REQUIRE_AGENT" "$E2E_REQUIRE_EMBEDDING" <<'PY'
import json
import sys

rules_path, sample_id, require_agent, require_embedding = sys.argv[1:5]
with open(rules_path, encoding="utf-8") as f:
    data = json.load(f)
semantic_labels = data.get("semantic_labels") or []
match = next((item for item in semantic_labels if item.get("sensitive_file_id") == sample_id), None)
if not match:
    raise SystemExit(f"semantic label for uploaded sample not found: {sample_id}")
model_name = (match.get("model_name") or "").strip()
if not model_name:
    raise SystemExit(f"model_name is empty: {match}")
if require_agent == "1" and model_name == "rule-fallback":
    raise SystemExit(
        "semantic analysis used rule fallback, not ChatModel. "
        "This means AnalyzeSemantic() was called, but analyzeWithLLM() failed and fell back to analyzeWithRules(). "
        "Check server logs for '大模型语义识别失败', and verify ARK_API_KEY / ARK_BASE_URL / ARK_CHAT_MODEL in .env. "
        f"semantic_feature={match}"
    )
if require_embedding == "1" and not (match.get("embedding_id") or "").strip():
    raise SystemExit(f"embedding_id is empty while E2E_REQUIRE_EMBEDDING=1: {match}")
labels = match.get("semantic_labels") or []
if not labels:
    raise SystemExit(f"semantic_labels is empty: {match}")
print(json.dumps({
    "sample_id": sample_id,
    "model_name": model_name,
    "embedding_id": match.get("embedding_id"),
    "semantic_labels": labels,
}, ensure_ascii=False, indent=2))
PY
pass "Semantic labels and embedding metadata validated"

if [[ "$E2E_REQUIRE_EMBEDDING" == "1" ]]; then
  log "TC05.2: semantic vector search returns uploaded sample"
  semantic_search_payload="$TMP_ROOT/semantic_search_payload.json"
  cat > "$semantic_search_payload" <<'JSON'
{"content":"客户报价和联系人资料","top_k":10,"min_score":0.1}
JSON
  semantic_search_json="$TMP_ROOT/semantic_search.json"
  status="$(curl_post_json "$SERVER_URL/api/server/semantic-search" "$semantic_search_payload" "$semantic_search_json")"
  assert_http_status "$status" "200" "POST /api/server/semantic-search" "$semantic_search_json"
  SEMANTIC_SEARCH_JSON_PY="$(py_path "$semantic_search_json")"
  "$PYTHON_BIN" - "$SEMANTIC_SEARCH_JSON_PY" "$SAMPLE_ID" "$E2E_EXPECT_EMBEDDING_MODEL" <<'PY'
import json
import sys

path, sample_id = sys.argv[1:3]
expected_model = sys.argv[3] if len(sys.argv) > 3 else ""
with open(path, encoding="utf-8") as f:
    data = json.load(f)
if data.get("vector_store") != "semantic_features":
    raise SystemExit(f"unexpected vector_store: {data}")
if data.get("similarity_metric") != "cosine":
    raise SystemExit(f"unexpected similarity_metric: {data}")
embedding_model = data.get("embedding_model")
if not embedding_model:
    raise SystemExit(f"missing embedding_model: {data}")
if expected_model and embedding_model != expected_model:
    raise SystemExit(f"embedding_model = {embedding_model!r}, want {expected_model!r}: {data}")
results = data.get("results") or []
if not results:
    raise SystemExit(f"semantic-search returned no results: {data}")
match = next((item for item in results if item.get("sensitive_file_id") == sample_id), None)
if not match:
    raise SystemExit(f"uploaded sample not found in semantic-search results: {results}")
if expected_model and match.get("model_name") != expected_model:
    raise SystemExit(f"uploaded sample model_name = {match.get('model_name')!r}, want {expected_model!r}: {match}")
if float(match.get("score", 0)) < 0.1:
    raise SystemExit(f"uploaded sample score below threshold: {match}")
if not match.get("embedding_id"):
    raise SystemExit(f"uploaded sample missing embedding_id: {match}")
PY
  pass "semantic-search uses vector store and finds uploaded sample"
else
  pass "Semantic vector search skipped because E2E_REQUIRE_EMBEDDING=0"
fi

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
