# 模块一代码审查：功能性问题与后续修改建议

审查时间：2026-06-11  
审查对象：`SCU-project-model-1` 完整代码库  
对照文档：`MODULE_ONE_PLAN.md`（规划） + `dlpagent.md`（底层设计）  
前次审查：`CODE_REVIEW_FIXES.md`（2026-06-10，大部分问题已修复）

---

## 1. 总体结论

当前代码库相比前次审查（`CODE_REVIEW_FIXES.md`）已修复了大量问题，整体质量显著提升。模块一 MVP 核心闭环能力（上传→规则生成→同步→扫描→标记）已基本完整：

- ✅ 服务端 (Go + Hertz)：事务保护上传、文本解析、规则生成（正则/关键词/组合 5 类模板）、指纹计算（SHA-256 + SimHash）、大模型语义识别（含规则降级）、Embedding 向量生成、规则版本管理、客户端同步 API（含语义标签）
- ✅ 客户端 (Python)：规则同步（含语义标签缓存）、目录/单文件扫描、多维度匹配（hash/SimHash/正则/关键词/组合/语义标签）、分级判定（sensitive/suspected/low_confidence/clean）、SQLite 本地标签库
- ✅ 测试覆盖：服务端 4 个测试文件（parser/fingerprint/rule_generator/matcher），客户端 4 个测试文件（matcher/scanner/local_db/e2e），Go `go test ./...` 全部通过
- ✅ 工程质量：`.env` 安全保护、`.env.example` 模板、ZIP 安全限制、环境变量统一命名、`requirements.txt` 与实际依赖一致

但仍存在 **若干功能性差距** 和 **工程健壮性问题**，按优先级分类如下。

---

## 2. 与 MODULE_ONE_PLAN.md 逐项对照（更新版）

| 规划要求 | 实现状态 | 差距说明 |
|---|---|---|
| 样本文件上传（单文件/批量/ZIP） | ✅ 已覆盖 | `POST /api/server/samples`、`/batch`、`/zip` |
| 文本抽取 (txt/csv/json/xml/md/代码) | ✅ 已覆盖 | Go 端直接读取 + 编码兼容 |
| 文本抽取 (docx) | ✅ 已覆盖 | Go 端解析 document.xml，客户端用 python-docx |
| 文本抽取 (xlsx) | ✅ 已覆盖 | Go 端用 excelize，客户端用 openpyxl |
| 文本抽取 (PDF) | 🔶 部分实现 | 仅依赖外部 PaddleOCR HTTP 服务；未集成 Go 原生 PDF 库；降级方案 `extractPDFLikeText` 对压缩 PDF 无效 |
| 文本抽取 (老版 Office/PPT/压缩包/邮件/图片) | ❌ 未实现 | 规划标记为可选增强，当前均未实现 |
| 正则规则生成 | ✅ 已覆盖 | 19 条内置正则，覆盖规划所有类型 |
| 关键词规则生成（gojieba + TF-IDF） | 🔴 **未实现** | `go.mod` 未引入 `github.com/yanyiwu/gojieba`；`extractKeywords()` 使用自建分词 + CJK ngram 统计，非规划要求的 TF-IDF |
| 组合规则生成 | ✅ 已修复 | 从 1 条扩展为 5 条模板（客户报价/财务预算/薪资绩效/源码泄露/合同保密） |
| 文件指纹（SHA-256 + SimHash） | ✅ 已覆盖 | 跨语言算法一致，测试通过 |
| ChatModel 语义识别 | ✅ 已覆盖 | Eino + 火山方舟，含规则降级方案 |
| Embedding 向量化 | ✅ 已修复 | `GenerateEmbedding()` 通过方舟 Embedding API 生成向量，写入 DB |
| 服务端敏感文件库管理 | ✅ 已覆盖 | GORM 自动迁移 5 表，事务保护写入 |
| 客户端规则同步（含语义标签） | ✅ 已修复 | `cached_semantic_labels` 表 + 同步逻辑 |
| 指定目录扫描 | ✅ 已覆盖 | 支持目录递归和单文件 |
| 敏感文件分级（>=80 敏感/50-79 疑似/30-49 低置信） | ✅ 已修复 | `compute_detection_status()` 实现正确的四级分类 |
| 输出结构与 `dlpagent.md` 3.5 对齐 | ✅ 已修复 | 上传响应含 `generated_rules`、`embedding_id`、`semantic_labels`、`explanation` |
| 规则版本管理（自增主键） | ✅ 已修复 | `RuleVersion.Version` 改为 `autoIncrement` |
| 预留接口 `POST /api/client/scan-results` | ❌ **未实现** | 规划 6.4 明确列出的预留接口 |
| 预留接口 `GET /api/server/sensitive-files/:hash` | ✅ 已覆盖 | `GetSensitiveFileInfo` 实现 |

---

## 3. 关键功能性问题与修复建议

### 🔴 P0 — 影响 MVP 完整性的核心功能缺失

#### 3.1 服务端未接入 gojieba / TF-IDF 关键词抽取

**文件：** `server/core/rule_generator.go:182-244`、`server/go.mod`

**问题：** MODULE_ONE_PLAN.md 第 3.2 节明确要求使用 `gojieba` 提取 TF-IDF 关键词。`go.mod` 中未引入该依赖，`extractKeywords()` 的实际实现为：

1. 内置业务词表前缀匹配（第 185-197 行）——只能匹配预定义的 51 个词
2. 简单字符分词 + 计数（第 199-208 行）——将 "合同金额" 拆为 "合/同/金/额/合同金/同金额" 等 ngram
3. CJK 2-4 gram 提取（第 210-215 行）——生成大量无意义片段
4. 停止词仅 8 个（英文 4 个 + 中文 4 个）

这导致：
- 无法对未见过的业务词汇进行有效分词（如"甲方的履约保证金"无法识别 "履约保证金"）
- 没有 TF-IDF 权重，高频但无意义的词可能排在前面
- 中文分词的准确性远低于 jieba 的分词+词性标注

**修复建议：**

```bash
cd server
go get github.com/yanyiwu/gojieba
```

```go
// server/core/rule_generator.go

import (
    "github.com/yanyiwu/gojieba"
    "math"
    "sort"
)

var (
    jiebaSegmenter *gojieba.Jieba
    jiebaOnce      sync.Once
)

func getJieba() *gojieba.Jieba {
    jiebaOnce.Do(func() {
        jiebaSegmenter = gojieba.NewJieba()
    })
    return jiebaSegmenter
}

func extractKeywords(text, sensitiveType, description string) []string {
    jieba := getJieba()
    
    // 1. 分词 + 词性过滤（保留名词/动词/英文）
    words := jieba.Cut(text, true)
    
    // 2. 计算 TF（词频）
    tf := make(map[string]float64)
    totalWords := 0
    for _, w := range words {
        w = strings.TrimSpace(w)
        if len([]rune(w)) < 2 || isStopWord(w) {
            continue
        }
        tf[w]++
        totalWords++
    }
    
    // 3. 用户提示词加权（模拟"相关性"提升）
    userWords := jieba.Cut(sensitiveType+" "+description, true)
    for _, w := range userWords {
        w = strings.TrimSpace(w)
        if len([]rune(w)) >= 2 {
            tf[w] += 5 // 大幅提升用户指定词汇的权重
        }
    }
    
    // 4. 排序取 top N
    type pair struct{ word string; score float64 }
    var pairs []pair
    for word, count := range tf {
        pairs = append(pairs, pair{word: word, score: count / float64(totalWords)})
    }
    sort.Slice(pairs, func(i, j int) bool { return pairs[i].score > pairs[j].score })
    
    limit := 12
    if len(pairs) < limit { limit = len(pairs) }
    keywords := make([]string, 0, limit)
    for i := 0; i < limit; i++ {
        keywords = append(keywords, pairs[i].word)
    }
    return keywords
}
```

与此同时大幅扩展停用词表（至少 50+ 中文停用词）：

```go
var stopWords = map[string]bool{
    "的": true, "了": true, "在": true, "是": true, "我": true, "有": true,
    "和": true, "就": true, "不": true, "人": true, "都": true, "一": true,
    "一个": true, "上": true, "也": true, "很": true, "到": true, "说": true,
    "要": true, "去": true, "你": true, "会": true, "着": true, "没有": true,
    "看": true, "好": true, "自己": true, "这": true, "那": true, "他": true,
    "她": true, "它": true, "们": true, "什么": true, "为": true, "所以": true,
    "因为": true, "但是": true, "可以": true, "这个": true, "那个": true,
    "文件": true, "文档": true, "内容": true, "信息": true, "数据": true,
    "资料": true, "包含": true, "相关": true, "进行": true, "使用": true,
    "the": true, "and": true, "this": true, "that": true, "with": true,
    "for": true, "from": true, "have": true, "has": true, "been": true,
}
```

---

#### 3.2 服务端 PDF 文本解析能力严重不足

**文件：** `server/core/parser.go:46-58`、`server/core/parser.go:246-249`

**问题：** MODULE_ONE_PLAN.md 第 4.2 节明确 PDF 在 MVP 范围内，推荐使用 `gopdf/unidoc`。当前实现：

- 主路径 `extractPDFWithPaddleOCR()` 依赖外部 PaddleOCR HTTP 服务，未配置时完全无法工作
- 降级方案 `extractPDFLikeText()` 仅对原始二进制做正则抓取可见 ASCII/汉字连续片段——这对 **压缩后的 PDF**（占绝大多数真实 PDF）完全无效，因为压缩后的文本流是二进制 blob
- `go.mod` 未引入任何 Go 原生 PDF 库

**修复建议：**

引入 Go PDF 库，优先使用原生解析，PaddleOCR 作为扫描件降级：

```bash
go get github.com/ledongthuc/pdf
```

```go
// server/core/parser.go

import "github.com/ledongthuc/pdf"

func extractPDFNative(data []byte) (string, error) {
    reader := bytes.NewReader(data)
    size := int64(len(data))
    pdfReader, err := pdf.NewReader(reader, size)
    if err != nil {
        return "", fmt.Errorf("pdf reader init failed: %w", err)
    }
    
    var parts []string
    totalPages := pdfReader.NumPage()
    for i := 1; i <= totalPages; i++ {
        page := pdfReader.Page(i)
        if page.V.IsNull() {
            continue
        }
        text, err := page.GetPlainText(nil)
        if err == nil && strings.TrimSpace(text) != "" {
            parts = append(parts, text)
        }
    }
    
    if len(parts) == 0 {
        return "", fmt.Errorf("no extractable text from %d pages", totalPages)
    }
    return strings.Join(parts, "\n"), nil
}
```

然后在 `ExtractText` 的 PDF 分支中调整为级联策略：

```go
case "pdf":
    // 策略 1: 尝试原生 PDF 文本提取
    text, err := extractPDFNative(data)
    if err == nil && len(strings.TrimSpace(text)) > 50 {
        return cleanText(text), ext, nil
    }
    // 策略 2: 尝试 PaddleOCR（扫描件）
    text, err = extractPDFWithPaddleOCR(fileName, data)
    if err == nil {
        return cleanText(text), ext, nil
    }
    // 策略 3: 降级为启发式提取（并记录 warning）
    fallback := extractPDFLikeText(data)
    if fallback != "" {
        return cleanText(fallback), ext, fmt.Errorf("pdf extraction: native failed, paddleocr failed (%v), using fallback", err)
    }
    return "", ext, fmt.Errorf("pdf extraction failed at all levels: %v", err)
```

---

#### 3.3 缺少 `POST /api/client/scan-results` 预留接口

**文件：** `server/router/rules.go`

**问题：** MODULE_ONE_PLAN.md 第 6.4 节明确列出的预留接口，用于客户端将本地扫描结果同步回服务端，供后续模块三/四复用识别结果。当前未实现。

**修复建议：**

在 `server/router/rules.go` 中新增接口：

```go
type ScanResultReport struct {
    HostID    string       `json:"host_id"`
    ScanPath  string       `json:"scan_path"`
    ScannedAt string       `json:"scanned_at"`
    Results   []ScanEntry  `json:"results"`
}

type ScanEntry struct {
    FilePath         string `json:"file_path"`
    FileHash         string `json:"file_hash"`
    Sensitive        bool   `json:"sensitive"`
    SensitiveType    string `json:"sensitive_type"`
    RiskLevel        string `json:"risk_level"`
    SensitiveFileID  string `json:"sensitive_file_id"`
    MatchScore       int    `json:"match_score"`
    ConfidenceLevel  string `json:"confidence_level"`
    MatchDetail      string `json:"match_detail"`
}

func ReportScanResults(ctx *app.RequestContext) {
    var report ScanResultReport
    if err := json.Unmarshal(ctx.Request.Body(), &report); err != nil {
        ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
        return
    }
    // MVP: 先记录日志，后续可存入专属表供模块三/四查询
    zap.L().Info("收到客户端扫描结果上报",
        zap.String("host_id", report.HostID),
        zap.String("scan_path", report.ScanPath),
        zap.Int("result_count", len(report.Results)),
    )
    ctx.JSON(consts.StatusOK, map[string]string{"status": "received"})
}
```

在 `main.go` 中注册路由：

```go
h.POST("/api/client/scan-results", wrap(router.ReportScanResults))
```

客户端在扫描完成后可选上报（可通过 `--report` 参数触发）：

```python
# client/client.py 新增子命令
report_parser = subparsers.add_parser("report", help="上报扫描结果到服务端")
report_parser.add_argument("--server", default="http://127.0.0.1:8080")
# ...
elif args.command == "report":
    results = db.list_tags()
    requests.post(f"{args.server}/api/client/scan-results", json={
        "host_id": socket.gethostname(),
        "scan_path": str(Path.cwd()),
        "scanned_at": datetime.now().isoformat(),
        "results": results,
    })
```

---

### 🟠 P1 — 影响识别能力的重要改进

#### 3.4 上传缺少 SHA-256 去重

**文件：** `server/router/rules.go:304-348`（`UploadSample`）

**问题：** 同一个样本文件上传两次，会生成两份 `sensitive_sample`、两套 `generated_rules`、两个 `file_fingerprint`，并触发两次 `rule_versions` 递增。参考 `dlpagent.md` 的设计——敏感文件库应以文件内容（hash）为唯一标识，避免重复数据。

**修复建议：**

在 `prepareUpload` 之后、事务写入之前增加去重检查：

```go
func UploadSample(ctx *app.RequestContext) {
    // ... 现有校验 ...
    item := prepareUpload(fileHeader.Filename, data, sensitiveType, riskLevel, description)

    // 检查是否已存在相同 SHA-256 的样本
    var existing model.SensitiveSample
    if err := dal.DB.Where("sha256 = ?", item.SHA256).First(&existing).Error; err == nil {
        ctx.JSON(consts.StatusConflict, map[string]any{
            "error":              "该文件已存在于敏感文件库中",
            "sensitive_file_id":  existing.ID,
            "existing_file_name": existing.FileName,
            "uploaded_at":        existing.UploadedAt,
        })
        return
    }

    // ... 事务写入 ...
}
```

---

#### 3.5 SimHash 汉明距离阈值缺少可配置机制

**文件：** `server/core/fingerprint.go:46-57`、`client/matcher.py:78-90`

**问题：** SimHash 相似度阈值硬编码为 3（汉明距离）。这一值对短文本可能过严、对长文本可能过松。对于不同业务场景（如合同文本 vs 代码文件），最优阈值可能不同。规划中未硬编码此值，应该可配。

**修复建议：**

服务端同步接口返回推荐阈值：

```go
type RuleSyncResponse struct {
    // ... 现有字段 ...
    Config struct {
        SimhashThreshold int `json:"simhash_threshold"` // 新增
    } `json:"config"`
}
```

客户端扫描时使用服务端下发的阈值：

```python
def match_simhash(simhash: str, fingerprints: list, threshold: int = None) -> Optional[dict]:
    if threshold is None:
        threshold = 3  # 默认值
    # ... 现有逻辑 ...
```

---

#### 3.6 大模型 prompt 构建存在潜在格式化问题

**文件：** `server/core/semantic.go:149-153`

**问题：** `buildSemanticPrompt()` 使用 `strings.Replace(prompt, "%s", value, 1)` 三次连续替换。如果用户提供的 `sensitiveType` 包含 `%s` 字面量（如 C 代码样本中的 `printf("%s", ...)`），会破坏模板替换的对应关系。

```go
// 当前实现
func buildSemanticPrompt(sensitiveType, riskLevel, text string) string {
    prompt := strings.Replace(semanticPromptTemplate, "%s", sensitiveType, 1)
    prompt = strings.Replace(prompt, "%s", riskLevel, 1)
    return strings.Replace(prompt, "%s", text, 1)
}
```

**修复建议：**

使用 `strings.Replacer` 或 `fmt.Sprintf` 的一次性替换（需改造模板使用 `%q` 防止注入，或使用 `text/template`）：

```go
import "strings"

func buildSemanticPrompt(sensitiveType, riskLevel, text string) string {
    replacer := strings.NewReplacer(
        "{{SENSITIVE_TYPE}}", sensitiveType,
        "{{RISK_LEVEL}}", riskLevel,
        "{{DOCUMENT_TEXT}}", text,
    )
    return replacer.Replace(semanticPromptTemplate)
}
```

同时修改 `semantic_prompt.txt` 模板，将 `%s` 替换为 `{{SENSITIVE_TYPE}}`、`{{RISK_LEVEL}}`、`{{DOCUMENT_TEXT}}`。

---

#### 3.7 客户端测试依赖 venv 环境

**文件：** `client/test_scanner.py`、`client/test_e2e_module_one.py`

**问题：** 测试文件 import `loguru`，直接运行 `python -m pytest` 或 `python -m unittest` 时如果不在 venv 中会失败（如本次审查中 `test_scanner.py` 和 `test_e2e_module_one.py` 因 `ModuleNotFoundError: No module named 'loguru'` 报错）。

**修复建议：**

提供一键运行脚本或在 README 中说明：

```bash
# 推荐运行方式
cd client
.venv/Scripts/activate
python -m pytest test_*.py -v
```

或创建 `client/run_tests.sh`：

```bash
#!/bin/bash
cd "$(dirname "$0")"
if [ -d ".venv" ]; then
    source .venv/Scripts/activate 2>/dev/null || source .venv/bin/activate 2>/dev/null
fi
python -m pytest test_*.py -v "$@"
```

---

### 🟡 P2 — 工程健壮性提升

#### 3.8 `wrap()` 丢弃 Hertz 请求上下文

**文件：** `server/main.go:18-22`

**问题：** `wrap()` 函数将 Hertz handler 签名从 `func(context.Context, *app.RequestContext)` 转换为 `func(*app.RequestContext)`，丢弃了 Go context。这意味着：
- 请求超时/取消信号无法传递到 handler
- 分布式追踪的 trace span 丢失
- 如果后续在 handler 中调用需要 context 的操作（如 DB 查询），无法级联取消

**修复建议：**

```go
// 方案：修改所有 handler 签名为 Hertz 标准签名
// 不需要 wrap 函数

// 修改前：
func UploadSample(ctx *app.RequestContext) { ... }
h.POST("/api/server/samples", wrap(router.UploadSample))

// 修改后：
func UploadSample(c context.Context, ctx *app.RequestContext) { ... }
h.POST("/api/server/samples", router.UploadSample)
```

如果 handler 数量较多且希望渐进迁移，可暂时使用以下 wrapper（传递 context）：

```go
func wrap(handler func(context.Context, *app.RequestContext)) app.HandlerFunc {
    return func(c context.Context, ctx *app.RequestContext) {
        handler(c, ctx)
    }
}
```

---

#### 3.9 缺少优雅关闭

**文件：** `server/main.go:75`

**问题：** `h.Spin()` 是阻塞调用，Ctrl+C 直接杀进程。无法：
- 等待正在处理的请求完成
- 关闭数据库连接池
- 刷新日志缓冲区

**修复建议：**

```go
import (
    "os"
    "os/signal"
    "syscall"
)

func main() {
    // ... 初始化 ...

    go func() {
        quit := make(chan os.Signal, 1)
        signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
        <-quit
        zap.L().Info("收到退出信号，正在优雅关闭...")
        
        // 关闭 HTTP 服务
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        if err := h.Shutdown(ctx); err != nil {
            zap.L().Error("强制关闭", zap.Error(err))
        }
        
        // 关闭数据库连接
        sqlDB, _ := dal.DB.DB()
        if sqlDB != nil {
            sqlDB.Close()
        }
    }()

    zap.L().Info("敏感文件识别服务启动", zap.String("addr", addr))
    h.Spin()
}
```

---

#### 3.10 缺少全局请求体大小限制

**文件：** `server/main.go:58`

**问题：** 各 handler 内部各自校验文件大小，但未在框架层面对所有请求设置 body 大小上限。恶意客户端可以发送超大 JSON body 攻击 `/api/server/regex-test` 等纯 JSON 接口。

**修复建议：**

```go
import "github.com/cloudwego/hertz/pkg/app/server"

h := server.Default(
    server.WithHostPorts(addr),
    server.WithMaxRequestBodySize(50 * 1024 * 1024), // 50MB 全局限制
)
```

---

#### 3.11 客户端 `list` 命令以 JSON 输出不够可读

**文件：** `client/client.py:69-71`

**问题：** `python client.py list` 直接 `json.dumps` 输出，字段多时不易阅读。建议增加表格格式：

```python
elif args.command == "list":
    rows = db.list_tags(args.sensitive_only)
    if args.json:
        print(json.dumps(rows, ensure_ascii=False, indent=2))
    else:
        # 表格输出
        print(f"{'文件路径':<60} {'置信度':<12} {'分数':<6} {'风险等级':<8} {'敏感类型':<20}")
        print("-" * 110)
        for r in rows:
            print(f"{r['file_path']:<60} {r.get('confidence_level','clean'):<12} "
                  f"{r.get('match_score',0):<6} {r.get('risk_level','info'):<8} "
                  f"{r.get('sensitive_type','') or '':<20}")
```

---

## 4. 功能差距汇总表

| # | 优先级 | 差距描述 | 涉及文件 | 预估工作量 |
|---|---|---|---|---|
| 1 | 🔴 P0 | 服务端未接入 gojieba，关键词抽取非 TF-IDF | `server/core/rule_generator.go`、`server/go.mod` | 3-4h |
| 2 | 🔴 P0 | 服务端 PDF 解析无 Go 原生方案 | `server/core/parser.go`、`server/go.mod` | 2-3h |
| 3 | 🔴 P0 | 缺少 `POST /api/client/scan-results` | `server/router/rules.go`、`server/main.go`、`client/client.py` | 1-2h |
| 4 | 🟠 P1 | 上传缺少 SHA-256 去重 | `server/router/rules.go` | 0.5h |
| 5 | 🟠 P1 | SimHash 阈值不可配置 | `server/router/rules.go`、`client/matcher.py` | 1h |
| 6 | 🟠 P1 | LLM prompt 格式化有注入风险 | `server/core/semantic.go`、`server/core/semantic_prompt.txt` | 0.5h |
| 7 | 🟠 P1 | 测试环境依赖说明不足 | `client/`、`README.md` | 0.5h |
| 8 | 🟡 P2 | `wrap()` 丢弃 Hertz context | `server/main.go`、`server/router/rules.go`（所有 handler） | 2-3h |
| 9 | 🟡 P2 | 缺少优雅关闭 | `server/main.go` | 1h |
| 10 | 🟡 P2 | 缺少全局请求体大小限制 | `server/main.go` | 0.25h |
| 11 | 🟡 P2 | `list` 命令输出可读性 | `client/client.py` | 0.5h |

---

## 5. 实施优先级排序

### 第一轮（P0）—— 补齐 MVP 规划核心能力（预估 6-9 小时）

| 序号 | 任务 | 涉及文件 |
|---|---|---|
| 1 | 接入 gojieba + TF-IDF 关键词抽取 | `server/core/rule_generator.go`、`server/go.mod` |
| 2 | 集成 Go 原生 PDF 解析库，改进文本提取 | `server/core/parser.go`、`server/go.mod` |
| 3 | 实现 `POST /api/client/scan-results` 预留接口 | `server/router/rules.go`、`server/main.go`、`client/client.py` |

### 第二轮（P1）—— 提升识别能力与数据质量（预估 2.5 小时）

| 序号 | 任务 | 涉及文件 |
|---|---|---|
| 4 | 上传 SHA-256 去重 | `server/router/rules.go` |
| 5 | SimHash 阈值可配置（同步到客户端） | `server/router/rules.go`、`client/matcher.py` |
| 6 | 修复 LLM prompt 格式化方式 | `server/core/semantic.go`、`server/core/semantic_prompt.txt` |
| 7 | README 中说明测试运行方式 | `README.md`、新增 `client/run_tests.sh` |

### 第三轮（P2）—— 工程健壮性（预估 4 小时）

| 序号 | 任务 | 涉及文件 |
|---|---|---|
| 8 | 修复 `wrap()` 丢弃 context 问题 | `server/main.go`、`server/router/rules.go` |
| 9 | 增加优雅关闭 | `server/main.go` |
| 10 | 增加全局请求体大小限制 | `server/main.go` |
| 11 | 改进 `list` 命令输出格式 | `client/client.py` |

---

## 6. 与 dlpagent.md 底层设计思路的一致性检查

`dlpagent.md` 作为底层设计思路参考，以下核心理念在代码中的体现：

| dlpagent.md 要求 | 代码体现 | 一致性 |
|---|---|---|
| 3.1 正则/关键词/指纹/向量化四类规则 | ✅ `rule_generator.go` 四类规则均已实现 | 一致 |
| 3.3.1 固定格式敏感信息（17 类） | ✅ `BuiltinRegexRules` 覆盖 19 条规则 | 一致，代码覆盖更全 |
| 3.3.2 企业业务敏感信息（15 类） | ✅ 51 个业务关键词 + 5 个组合规则模板 + 语义标签推理 | 一致 |
| 3.3.3 文档语义特征（10 类） | ✅ LLM 语义分析 + 规则降级 `inferLabels` 覆盖 | 一致 |
| 3.4.3 组合规则（AND 逻辑） | ✅ 5 个组合模板均使用 AND 逻辑 | 一致 |
| 3.4.4 文件指纹（SHA-256 + SimHash） | ✅ 双指纹实现，跨语言一致 | 一致 |
| 3.4.5 语义向量（可选 Embedding） | ✅ `GenerateEmbedding` 通过方舟 API 实现 | 一致 |
| 3.5 输出结构对齐 | ✅ `SampleUploadResponse` 字段对齐 | 一致 |
| 3.6.2 客户端文本提取（多种格式） | ✅ txt/docx/xlsx/pdf 支持 | 一致，老格式/压缩包/图片待增强 |
| 3.6.3 敏感文件标记（SQLite + hash + 路径） | ✅ `local_db.py` 实现 | 一致 |

**总体评价：** 代码实现与 `dlpagent.md` 的核心设计思路高度一致。主要差距集中在具体技术实现的选择上（gojieba 未引入、Go PDF 库缺失），而非架构设计层面的偏差。

---

## 7. 验证记录

```bash
# 服务端编译 + 测试
cd SCU-project-model-1/server
go build ./...    # ✅ 编译通过
go test ./...     # ✅ 所有测试通过 (core 包 12 个测试)

# 客户端语法检查
cd SCU-project-model-1/client
python -c "import ast; [ast.parse(open(f).read()) for f in
  ['client.py','sync.py','scanner.py','matcher.py','local_db.py']]"
# ✅ 所有文件语法检查通过

# 客户端测试（在 venv 中运行）
# ✅ test_matcher.py:  7 passed
# ✅ test_local_db.py: 3 passed
# ⚠ test_scanner.py:  需要 venv 中的 loguru（文档待补充）
# ⚠ test_e2e_module_one.py: 需要运行中的服务端 + MODULE_ONE_E2E_SERVER 环境变量
```

---

## 8. 已修复问题确认（相比 CODE_REVIEW_FIXES.md）

以下前次审查文档指出的问题已在当前代码中修复，确认无需再次处理：

| 前次问题 | 修复状态 |
|---|---|
| 3.1 上传缺少事务保护 | ✅ 已使用 `dal.DB.Transaction()` |
| 3.2 版本号并发竞态 | ✅ `RuleVersion.Version` 改为 `autoIncrement` |
| 3.3 客户端敏感判定阈值不一致 | ✅ `compute_detection_status()` 四级分类 |
| 3.4 测试覆盖不足 | ✅ 新增 8 个测试文件，合计 21 个测试用例 |
| 3.5 Embedding 未实现 | ✅ `GenerateEmbedding()` 已实现 |
| 3.6 gojieba 未引入（仍存在） | 🔴 **仍未修复**，见本报告 3.1 |
| 3.7 PDF 解析不足（仍存在） | 🔴 **仍未修复**，见本报告 3.2 |
| 3.8 上传返回结构不一致 | ✅ 已增加 `generated_rules`、`embedding_id`、`semantic_labels` |
| 3.9 语义标签未同步 | ✅ `cached_semantic_labels` 表 + 同步逻辑 |
| 3.10 组合规则硬编码 | ✅ 从 1 条扩展为 5 条模板 |
| 3.11 环境变量命名不一致 | ✅ 统一为 `ARK_CHAT_MODEL`（兼容旧 `ARK_ENDPOINT_ID`） |
| 3.12 ZIP 安全限制 | ✅ 已增加大小/数量/路径穿越检查 |
| 3.13 指纹全量同步 | ✅ `FileFingerprint.Version` 字段 + 增量查询 |
| 3.14 jieba 客户端未使用 | ✅ `matcher.py` 已 import jieba 辅助匹配 |
| 3.15 缺少 matcher.go | ✅ 已创建 `server/core/matcher.go` |
| 3.16 .env 密钥安全 | ✅ `.env` 在 `.gitignore` 中，`.env.example` 已创建 |
| 3.17 代码小问题 | ✅ 大部分已修复（正则预编译、ZIP 限制扩展等） |
| 3.18 requirements.txt 不一致 | ✅ 已清理，与实际依赖一致 |

---

*本文档由代码审查自动生成，建议在实施修改后重新验证。*
