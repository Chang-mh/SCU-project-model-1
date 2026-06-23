const $ = (selector) => document.querySelector(selector);

const state = {
  lastRules: null,
  lastUpload: null,
  lastSampleHash: "",
  checks: new Map(),
};

const sampleContent = `身份证 11010119900307123X
手机号 13800138000
银行卡 4111111111111111
邮箱 test@example.com
地址 四川省成都市高新区天府大道88号
车牌 川A12345
护照 E12345678
社保号 社保 123456789012
税号 税号 91350100M000100Y43
统一社会信用代码 91350100M000100Y43
合同编号 HT-2026-ABC001
API_KEY = abcdefghijklmnop123456
access_token = abcdefghijklmnop1234567890
-----BEGIN PRIVATE KEY-----
password = SuperSecret123
db = jdbc:mysql://127.0.0.1:3306/app
private_ip = 192.168.1.10
域名 internal.example.com
报价 50万元`;

const tests = [
  ["TC01", "服务端连通性", "GET /api/client/rules?version=0", "live"],
  ["TC01.1", "固定格式敏感信息", "content-scan 命中内置规则", "live"],
  ["TC02", "样本上传", "生成规则、指纹、语义标签", "live"],
  ["TC03", "重复上传", "相同 sha256 返回 409 或 duplicates", "manual"],
  ["TC04", "敏感文件查询", "sha256 查询上传样本", "live"],
  ["TC05", "规则同步", "rules / fingerprints / config", "live"],
  ["TC05.1", "语义 Agent 元数据", "model_name / semantic_labels / embedding_id", "live"],
  ["TC05.2", "语义向量检索", "semantic_features + cosine", "live"],
  ["TC06-09", "客户端同步与 SQLite", "需 Python client / E2E 脚本", "client"],
  ["TC10-11", "扫描结果上报查询", "POST report + GET results", "live"],
  ["TC12", "不存在路径负向", "需 Python client 扫描命令", "client"],
];

function init() {
  const config = window.MODULE_ONE_DEMO_CONFIG || {};
  $("#apiBase").value = localStorage.getItem("moduleOneApiBase") ?? config.apiBase ?? "";
  $("#apiToken").value = localStorage.getItem("moduleOneToken") ?? config.defaultToken ?? "";
  $("#contentText").value = sampleContent;
  renderChecklist();
  bindEvents();
  loadRules();
}

function bindEvents() {
  $("#apiBase").addEventListener("change", () => localStorage.setItem("moduleOneApiBase", $("#apiBase").value.trim()));
  $("#apiToken").addEventListener("change", () => localStorage.setItem("moduleOneToken", $("#apiToken").value.trim()));
  $("#btnHealth").addEventListener("click", loadRules);
  $("#btnLoadRulesTop").addEventListener("click", loadRules);
  $("#btnRunBrowserChecks").addEventListener("click", runBrowserChecks);
  $("#btnContentScan").addEventListener("click", runContentScan);
  $("#btnUpload").addEventListener("click", uploadSample);
  $("#btnRuleSync").addEventListener("click", loadRules);
  $("#btnDuplicateHint").addEventListener("click", uploadSample);
  $("#btnSemanticSearch").addEventListener("click", semanticSearch);
  $("#btnReportMock").addEventListener("click", reportMockScan);
  $("#btnListReports").addEventListener("click", listReports);
  $("#btnSensitiveQuery").addEventListener("click", querySensitive);
  $("#btnSensitiveList").addEventListener("click", listSensitiveFiles);
}

function apiBase() {
  return $("#apiBase").value.trim().replace(/\/$/, "");
}

function headers(extra = {}) {
  const token = $("#apiToken").value.trim();
  return token ? { ...extra, Authorization: `Bearer ${token}` } : extra;
}

async function request(method, path, { body, json = true, label = "API request" } = {}) {
  const url = `${apiBase()}${path}`;
  const options = { method, headers: headers() };
  if (json && body !== undefined) {
    options.headers = headers({ "Content-Type": "application/json" });
    options.body = JSON.stringify(body);
  } else if (body !== undefined) {
    options.body = body;
  }
  const started = performance.now();
  let data;
  let text = "";
  let status = 0;
  try {
    const response = await fetch(url, options);
    status = response.status;
    text = await response.text();
    try { data = text ? JSON.parse(text) : null; } catch { data = text; }
    showEvidence({ label, method, url, status, ms: Math.round(performance.now() - started), request: body instanceof FormData ? formDataSummary(body) : body, response: data });
    if (!response.ok) {
      const err = new Error(`HTTP ${status}`);
      err.status = status;
      err.data = data;
      throw err;
    }
    return data;
  } catch (err) {
    if (!status) showEvidence({ label, method, url, status: "NETWORK_ERROR", request: body instanceof FormData ? formDataSummary(body) : body, response: String(err) });
    throw err;
  }
}

function formDataSummary(form) {
  const out = {};
  for (const [key, value] of form.entries()) out[key] = value instanceof File ? { name: value.name, size: value.size, type: value.type } : value;
  return out;
}

function showEvidence(payload) {
  $("#evidenceMeta").textContent = `${payload.method} · ${payload.status} · ${payload.ms ?? "—"}ms`;
  $("#evidenceBox").textContent = JSON.stringify(payload, null, 2);
}

function setServerStatus(ok, text) {
  const el = $("#serverStatus");
  el.classList.toggle("error", !ok);
  el.innerHTML = `<i></i> ${text}`;
}

function setCheck(id, status) {
  state.checks.set(id, status);
  renderChecklist();
}

function renderChecklist() {
  $("#testChecklist").innerHTML = tests.map(([id, title, desc, kind], index) => {
    const status = state.checks.get(id) || (kind === "client" ? "client" : kind === "manual" ? "manual" : "pending");
    const label = status === "pass" ? "PASS" : status === "fail" ? "FAIL" : status === "client" ? "CLIENT" : status === "manual" ? "MANUAL" : "WAIT";
    const cls = status === "pass" ? "pass" : status === "fail" ? "fail" : status === "client" || status === "manual" ? "warn" : "";
    return `<div class="test-item"><span class="idx">${String(index + 1).padStart(2, "0")}</span><div><b>${id} · ${title}</b><small>${desc}</small></div><span class="badge ${cls}">${label}</span></div>`;
  }).join("");
}

function cards(items) {
  if (!items.length) return `<div class="result-card"><b>暂无结果</b><small>等待 API 返回。</small></div>`;
  return items.join("");
}

function resultCard(title, subtitle, badge = "") {
  return `<div class="result-card"><b>${escapeHtml(title)} ${badge}</b><small>${escapeHtml(subtitle ?? "")}</small></div>`;
}

function escapeHtml(value) {
  return String(value ?? "").replace(/[&<>"]/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "\"": "&quot;" }[char]));
}

async function loadRules() {
  try {
    const data = await request("GET", "/api/client/rules?version=0", { label: "规则同步连通性" });
    state.lastRules = data;
    const rules = data.rules || [];
    const fingerprints = data.fingerprints || [];
    const semantic = data.semantic_labels || [];
    $("#metricRules").textContent = rules.length;
    $("#metricFingerprints").textContent = fingerprints.length;
    $("#metricSemantic").textContent = semantic.length;
    renderOpsTables({ rules, semantic });
    setServerStatus(true, `ONLINE · v${data.latest_version ?? 0}`);
    setCheck("TC01", "pass");
    setCheck("TC05", rules.length || fingerprints.length || data.config ? "pass" : "fail");
    $("#uploadSummary").insertAdjacentHTML("beforeend", `<div class="summary-card"><b>规则同步</b><code>rules=${rules.length}<br/>fingerprints=${fingerprints.length}<br/>semantic=${semantic.length}<br/>simhash=${data.config?.simhash_threshold ?? "—"}</code></div>`);
    return data;
  } catch (err) {
    setServerStatus(false, `OFFLINE · ${err.message}`);
    setCheck("TC01", "fail");
  }
}

async function runContentScan() {
  try {
    const data = await request("POST", "/api/server/content-scan", { label: "固定格式敏感信息扫描", body: { content: $("#contentText").value } });
    const results = data.results || [];
    $("#contentResults").innerHTML = cards(results.slice(0, 16).map((r) => resultCard(r.rule_name, `${r.risk_level} · ${r.matches?.length || 0} hits`, `<span class="badge ${r.risk_level === "high" ? "fail" : "pass"}">${r.risk_level}</span>`)));
    setCheck("TC01.1", results.length >= 6 ? "pass" : "fail");
    updateVerdict(results.length ? "HIGH" : "CLEAN", `固定格式扫描命中 ${results.length} 类规则。`);
  } catch {
    setCheck("TC01.1", "fail");
  }
}

async function uploadSample() {
  const file = $("#sampleFile").files[0];
  if (!file) {
    alert("请先选择一个 txt/docx/xlsx/zip 等样本文件。可使用 samples/customer.txt。 ");
    return;
  }
  const form = new FormData();
  form.append("file", file);
  form.append("sensitive_type", $("#sensitiveType").value.trim());
  form.append("risk_level", $("#riskLevel").value);
  form.append("description", $("#sampleDescription").value.trim());
  try {
    const data = await request("POST", "/api/server/samples", { json: false, body: form, label: "上传敏感样本" });
    state.lastUpload = data;
    state.lastSampleHash = data.fingerprint?.sha256 || "";
    $("#sensitiveQuery").value = state.lastSampleHash;
    renderUpload(data);
    setCheck("TC02", data.sensitive_file_id && data.generated_rules_count > 0 ? "pass" : "fail");
    setCheck("TC05.1", (data.semantic_labels || []).length ? "pass" : "fail");
    updateVerdict((data.risk_level || "HIGH").toUpperCase(), data.explanation || "样本上传后已生成规则、指纹和语义标签。 ");
    loadRules();
  } catch (err) {
    if (err.status === 409) {
      setCheck("TC03", "pass");
      $("#uploadSummary").innerHTML = `<div class="summary-card"><b>重复上传命中</b><code>${escapeHtml(JSON.stringify(err.data?.duplicates || err.data, null, 2))}</code></div>`;
      return;
    }
    setCheck("TC02", "fail");
  }
}

function renderUpload(data) {
  const rules = data.generated_rules || [];
  $("#agentModelName").textContent = data.semantic_labels?.length ? (data.embedding_id ? "semantic + embedding" : "semantic labels") : "rule-fallback";
  $("#uploadSummary").innerHTML = [
    `<div class="summary-card"><b>样本 ID</b><code>${escapeHtml(data.sensitive_file_id)}</code></div>`,
    `<div class="summary-card"><b>规则版本</b><code>v${data.rule_version}<br/>count=${data.generated_rules_count}</code></div>`,
    `<div class="summary-card"><b>指纹</b><code>sha256=${escapeHtml(data.fingerprint?.sha256)}<br/>simhash=${escapeHtml(data.fingerprint?.simhash)}</code></div>`,
    `<div class="summary-card"><b>语义标签</b><code>${escapeHtml((data.semantic_labels || []).join(" / "))}<br/>embedding=${escapeHtml(data.embedding_id || "未生成")}</code></div>`,
    ...rules.slice(0, 8).map((rule) => `<div class="summary-card"><b>${escapeHtml(rule.rule_type || rule.type)}</b><code>${escapeHtml(rule.rule_name || rule.sensitive_type)}<br/>${escapeHtml(JSON.stringify(rule.content || {}).slice(0, 160))}</code></div>`),
  ].join("");
}

async function semanticSearch() {
  try {
    const data = await request("POST", "/api/server/semantic-search", { label: "Semantic Agent 向量检索", body: { content: $("#semanticQuery").value, top_k: 10, min_score: 0.1 } });
    $("#embeddingModel").textContent = data.embedding_model || "unknown";
    const results = data.results || [];
    $("#semanticResults").innerHTML = cards(results.map((r) => `<div class="result-card"><b>${escapeHtml(r.file_name || r.sensitive_file_id)} <span class="badge pass">${Number(r.score || 0).toFixed(3)}</span></b><small>${escapeHtml(r.sensitive_type)} · ${escapeHtml((r.semantic_labels || []).join(" / "))}<br/>${escapeHtml(r.embedding_id)}</small><div class="score-bar"><span style="width:${Math.max(4, Math.min(100, Number(r.score || 0) * 100))}%"></span></div></div>`));
    setCheck("TC05.2", results.length ? "pass" : "fail");
    updateVerdict(results.length ? "SENSITIVE" : "LOW", `语义检索返回 ${results.length} 条结果，模型：${data.embedding_model || "unknown"}。`);
  } catch (err) {
    $("#semanticResults").innerHTML = resultCard("Embedding 模型未就绪", err.data?.error || err.message, `<span class="badge warn">503</span>`);
    setCheck("TC05.2", err.status === 503 ? "manual" : "fail");
  }
}

async function reportMockScan() {
  const upload = state.lastUpload || {};
  const hash = state.lastSampleHash || $("#sensitiveQuery").value.trim() || "demo-hash";
  const report = {
    host_id: `frontend-demo-${Date.now()}`,
    scan_path: "browser://module-one-demo",
    scanned_at: new Date().toISOString(),
    results: [{
      file_path: upload.file_name || "customer_frontend_demo.txt",
      file_hash: hash,
      sensitive: true,
      sensitive_type: upload.sensitive_type || "客户资料",
      risk_level: upload.risk_level || "high",
      sensitive_file_id: upload.sensitive_file_id || "frontend-demo",
      match_score: 100,
      confidence_level: "sensitive",
      match_detail: { frontend_demo: true, sha256_hit: Boolean(state.lastSampleHash), agent_note: "浏览器模拟上报，用于验证接口证据" },
    }],
  };
  try {
    const data = await request("POST", "/api/client/scan-results", { label: "扫描结果上报", body: report });
    $("#reportResults").innerHTML = resultCard("上报成功", `report_id=${data.report_id} · received=${data.received}`, `<span class="badge pass">received</span>`);
    setCheck("TC10-11", "pass");
  } catch {
    setCheck("TC10-11", "fail");
  }
}

async function listReports() {
  try {
    const data = await request("GET", "/api/server/scan-results?sensitive_only=true&limit=20", { label: "查询服务端扫描结果" });
    const results = data.results || [];
    renderReportsTable(results);
    $("#reportResults").innerHTML = cards(results.slice(0, 10).map((r) => resultCard(r.file_path, `${r.confidence_level} · score=${r.match_score} · ${r.risk_level}`, `<span class="badge pass">${r.sensitive ? "sensitive" : "clean"}</span>`)));
    setCheck("TC10-11", "pass");
  } catch {
    setCheck("TC10-11", "fail");
  }
}

async function querySensitive() {
  const value = $("#sensitiveQuery").value.trim();
  if (!value) return alert("请先输入 sha256 或文件名；上传样本后会自动填入 sha256。 ");
  const isHash = /^[a-f0-9]{64}$/i.test(value);
  try {
    const data = isHash
      ? await request("GET", `/api/server/sensitive-files/${encodeURIComponent(value)}`, { label: "按 sha256 查询敏感文件" })
      : await request("POST", "/api/server/sensitive-files/query", { label: "按文件名查询敏感文件", body: { file_name: value } });
    $("#sensitiveResults").innerHTML = resultCard(data.file_name || data.sensitive_file_id || "查询完成", `${data.sensitive_type || ""} · ${data.risk_level || ""}`, `<span class="badge pass">${data.sensitive === false ? "clean" : "hit"}</span>`);
    setCheck("TC04", "pass");
  } catch {
    setCheck("TC04", "fail");
  }
}

async function listSensitiveFiles() {
  try {
    const data = await request("GET", "/api/server/sensitive-files?keyword=客户", { label: "敏感文件列表检索" });
    const list = Array.isArray(data) ? data : data.results || [];
    $("#sensitiveResults").innerHTML = cards(list.slice(0, 10).map((r) => resultCard(r.file_name, `${r.sensitive_type} · ${r.risk_level}`, `<span class="badge pass">sample</span>`)));
  } catch (err) {
    $("#sensitiveResults").innerHTML = resultCard("查询失败", err.message, `<span class="badge fail">fail</span>`);
  }
}

async function runBrowserChecks() {
  await loadRules();
  await runContentScan();
  await semanticSearch();
  await listReports();
}

function renderOpsTables({ rules = [], semantic = [] } = {}) {
  const ruleRows = rules.slice(0, 8).map((r) => `<tr><td><code>${escapeHtml(r.rule_id || "builtin")}</code></td><td>${escapeHtml(r.rule_type)}</td><td><span class="badge ${r.risk_level === "high" || r.risk_level === "critical" ? "fail" : "warn"}">${escapeHtml(r.risk_level || "medium")}</span></td></tr>`).join("");
  const semanticRows = semantic.slice(0, 8).map((s) => `<tr><td><code>${escapeHtml(s.sensitive_file_id)}</code></td><td>${escapeHtml(s.model_name || "rule-fallback")}</td><td><code>${escapeHtml(s.embedding_id || "—")}</code></td></tr>`).join("");
  const rulesBody = $("#rulesTable tbody");
  const semanticBody = $("#semanticTable tbody");
  if (rulesBody) rulesBody.innerHTML = ruleRows || `<tr><td colspan="3">暂无规则</td></tr>`;
  if (semanticBody) semanticBody.innerHTML = semanticRows || `<tr><td colspan="3">暂无语义特征</td></tr>`;
}

function renderReportsTable(results = []) {
  const rows = results.slice(0, 8).map((r) => `<tr><td><code>${escapeHtml(r.file_path)}</code></td><td>${escapeHtml(r.match_score ?? "—")}</td><td><span class="badge ${r.sensitive ? "fail" : "pass"}">${escapeHtml(r.confidence_level || "unknown")}</span></td></tr>`).join("");
  const body = $("#reportsTable tbody");
  if (body) body.innerHTML = rows || `<tr><td colspan="3">暂无扫描结果</td></tr>`;
}

function updateVerdict(risk, text) {
  $("#verdictRisk").textContent = risk;
  $("#verdictText").textContent = text;
}

document.addEventListener("DOMContentLoaded", init);
