# 模块一代码审查与后续修改建议

审查时间：2026-06-10  
审查对象：`SCU-project-model-1` 完整代码库  
对照文档：`MODULE_ONE_PLAN.md`

---

## 1. 总体结论

当前代码库已基本实现模块一 MVP 的核心闭环能力：

- ✅ 服务端 (Go + Hertz)：样本上传、文本解析、规则生成、指纹计算、规则版本管理、客户端同步 API
- ✅ 客户端 (Python)：规则同步、目录扫描、hash/SimHash/正则/关键词/组合规则匹配、SQLite 本地标签库
- ✅ 大模型语义识别：已集成 Eino + 火山方舟 ChatModel，未配置时有规则降级方案
- ✅ 数据库：GORM + MySQL 自动迁移，表结构与规划基本一致

但仍存在 **功能性差距** 和 **工程质量问题**，按优先级分类如下。

---

## 2. 与 MODULE_ONE_PLAN.md 逐项对照表

| 规划要求 | 实现状态 | 差距说明 |
|---|---|---|
| 2.1 样本文件上传 | ✅ 已覆盖 | `POST /api/server/samples`，含单文件、批量、ZIP 上传 |
| 2.1 文本抽取 (txt/csv/json/xml/md/代码) | ✅ 已覆盖 | `server/core/parser.go:17-22` |
| 2.1 文本抽取 (docx) | ✅ 已覆盖 | Go 端直接解析 document.xml，客户端用 python-docx |
| 2.1 文本抽取 (xlsx) | ✅ 已覆盖 | Go 端用 excelize，客户端用 openpyxl |
| 2.1 文本抽取 (PDF) | 🔶 弱实现 | 服务端仅启发式抓取可见 ASCII/汉字，未用专业 PDF 库 |
| 2.1 文本抽取 (老版 Office/PPT/压缩包/邮件) | ❌ 未实现 | 规划标记为可选增强，已实现 ZIP 但缺其他格式 |
| 2.1 敏感规则生成 (正则) | ✅ 已覆盖 | 19 条内置正则规则，覆盖规划列出的主要类型 |
| 2.1 敏感规则生成 (关键词) | 🔶 部分 | 未使用 gojieba/TF-IDF，仅业务词表匹配 + 简单分词统计 |
| 2.1 敏感规则生成 (组合规则) | 🔶 部分 | 仅硬编码 1 条"客户报价组合"，未动态生成 |
| 2.1 敏感规则生成 (文件指纹) | ✅ 已覆盖 | SHA-256 + SimHash，服务端/客户端算法一致并通过测试 |
| 2.1 大模型语义识别 (ChatModel) | ✅ 已覆盖 | Eino + 火山方舟 OpenAI 兼容接口，含规则降级 |
| 2.1 大模型语义识别 (Embedding/向量化) | ❌ 未实现 | 未调用 Embedding 模型，DB 中 embedding 字段固定为空 |
| 2.1 服务端敏感文件库管理 | ✅ 已覆盖 | GORM 自动迁移 5 类表 |
| 2.1 客户端规则同步 | ✅ 已覆盖 | `GET /api/client/rules`，版本增量同步 |
| 2.1 指定目录扫描 | ✅ 已覆盖 | 支持目录递归和单文件扫描 |
| 2.1 敏感文件识别与本地标记 | 🔶 部分 | 识别逻辑 OK，但敏感/疑似/低置信分级不准确 |
| 4.4 输出结构对齐 | 🔶 部分 | 缺少 `generated_rules` 详情和 `embedding` 信息 |
| 4.5 规则版本管理 | 🔶 部分 | 版本号存在并发竞态风险 |
| 6.4 预留接口 | 🔶 部分 | 已实现 `GET /api/server/sensitive-files/:hash` 和查询接口，缺 `POST /api/client/scan-results` |

---

## 3. 关键功能性问题与修复建议

### 🔴 P0 — 影响 MVP 闭环可靠性

#### 3.1 上传入库缺少事务保护

**文件：** `server/router/rules.go:143-188`

**问题：** 每次上传写入 5 步（sample → rules → fingerprint → semantic_feature → rule_version），部分 `dal.DB.Create()` 返回值未被检查（第 171、180、188 行）。若中间某步失败，已写入数据无法回滚，导致数据库处于不一致状态。

**修复建议：**

```go
// 使用 GORM 事务包裹所有写入
err := dal.DB.Transaction(func(tx *gorm.DB) error {
    if err := tx.Create(&sample).Error; err != nil {
        return fmt.Errorf("保存样本失败: %w", err)
    }
    for _, rule := range rules {
        if err := tx.Create(&rule).Error; err != nil {
            return fmt.Errorf("保存规则失败: %w", err)
        }
    }
    if err := tx.Create(&fp).Error; err != nil {
        return fmt.Errorf("保存指纹失败: %w", err)
    }
    // ... semantic_feature, rule_version
    return nil
})
if err != nil {
    zap.L().Error("上传样本事务失败", zap.Error(err))
    ctx.JSON(consts.StatusInternalServerError, map[string]string{"error": err.Error()})
    return
}
```

---

#### 3.2 规则版本号并发竞态

**文件：** `server/router/rules.go:137-141`

**问题：** 版本号通过 `SELECT MAX(version) + 1` 生成。并发上传时两个请求可能读到相同的 `MAX(version)`，导致两个请求生成同一版本号。`rule_versions.version` 是主键，第二次 `Create` 会失败或被静默忽略。

**修复建议：**

方案 A（推荐）：将 `rule_versions.version` 改为自增主键：

```go
type RuleVersion struct {
    Version    int       `gorm:"primaryKey;autoIncrement"`
    ChangeType string    `gorm:"size:32"`
    CreatedAt  time.Time
}
```

方案 B：在事务中先 `INSERT` 版本记录获取自增 ID，再用该版本号写入规则：

```go
ver := model.RuleVersion{ChangeType: "upload", CreatedAt: time.Now()}
tx.Create(&ver) // version 由 DB 自增生成
for i := range rules {
    rules[i].Version = ver.Version
    tx.Create(&rules[i])
}
```

同样的问题也存在于 `UploadSamplesBatch`（第 376 行）和 `UploadZip`（第 563 行）。

---

#### 3.3 客户端敏感判定阈值与规划不一致

**文件：** `client/scanner.py:156-158`

**问题：** 规划定义 `>=80` 为敏感、`50-79` 为疑似、`30-49` 为低置信、`<30` 为未识别。当前代码将所有 `score >= 30` 的都标记为 `sensitive=True`，导致低置信文件也被标成敏感，产生大量误报。

**修复建议：**

```python
# client/scanner.py
def compute_detection_status(score: int) -> tuple[bool, str]:
    """返回 (sensitive, confidence_level)"""
    if score >= 80:
        return True, "sensitive"
    elif score >= 50:
        return False, "suspected"
    elif score >= 30:
        return False, "low_confidence"
    return False, "clean"

# 在 scan_file 中使用
sensitive, confidence = compute_detection_status(score)
```

同时在 `local_db.py` 的 `local_file_tags` 表中增加 `confidence_level` 字段：

```sql
ALTER TABLE local_file_tags ADD COLUMN confidence_level TEXT DEFAULT 'clean';
```

CLI 输出也需要区分：

```python
# client/client.py
label_map = {"sensitive": "[敏感]", "suspected": "[疑似]", "low_confidence": "[低置信]"}
for r in results:
    if r.get("confidence_level") != "clean":
        label = label_map.get(r["confidence_level"], "[命中]")
        print(f"{label} {r['file_path']} score={r['match_score']}")
```

---

#### 3.4 测试覆盖严重不足

**问题：** 当前仅有一条 SimHash 单元测试（`server/core/fingerprint_test.go`），以下核心路径均无测试。

**必须补充的测试：**

| 优先级 | 测试对象 | 测试内容 |
|---|---|---|
| P0 | `core.ExtractText` | txt/docx/xlsx 三种格式的基础文件正确解析 |
| P0 | `core.GenerateRules` | 包含手机号/邮箱/API Key 文本能生成正确正则规则 |
| P0 | `core.GenerateRules` | 包含客户+报价关键词文本触发组合规则 |
| P0 | `core.AnalyzeSemantic` | 规则降级模式下对客户资料文本给出正确标签 |
| P0 | `matcher.match_regex` | 手机号/身份证/邮箱正确匹配 |
| P0 | `matcher.match_keyword` | 关键词达到 min_hits 才命中 |
| P0 | `matcher.compute_score` | 各项命中分数累加正确 |
| P1 | `matcher.match_simhash` | 相似文本汉明距离 <=3 命中 |
| P1 | `local_db.upsert_file_tag` | 重复扫描同文件只更新时间不重复插入 |
| P1 | 上传接口集成测试 | 完整上传→入库→同步→扫描流程 |

---

### 🟠 P1 — 补齐规划中的核心识别能力

#### 3.5 语义向量化 (Embedding) 完全未实现

**文件：** `server/core/semantic.go`、`server/router/rules.go:77-94`

**问题：** 规划 4.3.5 明确要求使用 Embedding 模型生成文本向量。当前代码：
- 仅初始化 ChatModel，没有 Embedding 模型初始化和调用
- `saveSemanticFeature` 中 `Embedding` 字段固定为 `"[]"`（第 84 行）
- `EmbeddingID` 字段始终为空

**修复建议：**

1. 在 `semantic.go` 中新增 Embedding 模型初始化：

```go
var (
    embeddingModel     *openai.EmbeddingModel
    embeddingModelOnce sync.Once
    embeddingModelInit bool
)

func initEmbeddingModel() {
    embeddingModelOnce.Do(func() {
        apiKey := os.Getenv(EnvArkAPIKey)
        endpointID := os.Getenv(EnvArkEmbeddingEndpointID)
        if apiKey == "" || endpointID == "" {
            return
        }
        var err error
        embeddingModel, err = openai.NewEmbeddingModel(ctx, &openai.EmbeddingConfig{
            BaseURL: os.Getenv(EnvArkBaseURL),
            APIKey:  apiKey,
            Model:   endpointID,
        })
        if err != nil {
            zap.L().Error("Embedding 模型初始化失败", zap.Error(err))
            return
        }
        embeddingModelInit = true
    })
}

func computeEmbedding(text string) ([]float32, string, error) {
    initEmbeddingModel()
    if !embeddingModelInit {
        return nil, "", fmt.Errorf("embedding not configured")
    }
    // 截断文本，Embedding 模型通常有 token 限制
    truncated := truncateText(text, 8000)
    result, err := embeddingModel.EmbedStrings(ctx, []string{truncated})
    if err != nil {
        return nil, "", err
    }
    embID := "emb_" + randomHex(8)
    return result[0], embID, nil
}
```

2. 修改 `AnalyzeSemantic` 在上传时调用 embedding，将结果写入 DB。

3. 环境变量增加 `ARK_EMBEDDING_ENDPOINT_ID`（或直接复用 `ARK_EMBEDDING_MODEL`）。

---

#### 3.6 关键词抽取未使用 gojieba / TF-IDF

**文件：** `server/core/rule_generator.go:122-161`

**问题：** 规划 4.3.2 明确要求使用 `gojieba` 提取 TF-IDF 关键词。当前实现为：
- `go.mod` 未引入 `github.com/yanyiwu/gojieba`
- 关键词生成是内置业务词表前缀匹配（第 125-128 行）+ 简单字符分词计数（第 131-136 行）
- 没有 TF-IDF 权重计算，没有真正的分词（如"合同金额"会被拆成"合/同/金/额"）
- 停止词表仅 8 个英文词

**修复建议：**

1. 引入 gojieba：

```bash
go get github.com/yanyiwu/gojieba
```

2. 重构 `extractKeywords`：

```go
import "github.com/yanyiwu/gojieba"

var jiebaSegmenter *gojieba.Jieba

func init() {
    jiebaSegmenter = gojieba.NewJieba()
}

func extractKeywords(text, sensitiveType, description string) []string {
    // 1. 使用 gojieba 分词
    words := jiebaSegmenter.Cut(text, true)
    
    // 2. 过滤停用词、短词、数字日期
    filtered := filterMeaningfulWords(words)
    
    // 3. 计算 TF-IDF
    tfidf := computeTFIDF(filtered)
    
    // 4. 合并用户提供的 sensitive_type 和 description
    userWords := jiebaSegmenter.Cut(sensitiveType+" "+description, true)
    boostUserWords(tfidf, userWords)
    
    // 5. 排序取 top N
    return topKeywords(tfidf, 12)
}
```

3. 完善停用词表（至少包含常见的 50+ 中文停用词）。

---

#### 3.7 服务端 PDF 文本解析能力不足

**文件：** `server/core/parser.go:115-119`

**问题：** `extractPDFLikeText` 只是把整个 PDF 二进制当字符串处理，用正则抓取连续可见字符。这对二进制 PDF（绝大多数真实 PDF）几乎无效。

**修复建议：**

1. 引入 Go 端 PDF 库（如 `github.com/unidoc/unipdf/v3` 或 `github.com/ledongthuc/pdf`）：

```go
import "github.com/ledongthuc/pdf"

func extractPDFText(data []byte) (string, error) {
    reader := bytes.NewReader(data)
    pdfReader, err := pdf.NewReader(reader, int64(len(data)))
    if err != nil {
        return "", err
    }
    var parts []string
    for i := 1; i <= pdfReader.NumPage(); i++ {
        page := pdfReader.Page(i)
        if page.V.IsNull() {
            continue
        }
        text, err := page.GetPlainText(nil)
        if err == nil && text != "" {
            parts = append(parts, text)
        }
    }
    return strings.Join(parts, "\n"), nil
}
```

2. 在 `extractText` 中对 `.pdf` 调用新函数，失败时记录 warning 并返回部分文本。

---

#### 3.8 上传返回结构与规划不一致

**文件：** `server/router/rules.go:27-36`、`server/router/rules.go:192-201`

**问题：** 规划 4.4 定义的上传返回结构应包含：
- `generated_rules`（完整规则列表）→ 当前只返回 `generated_rules_count`
- `embedding`（或 `embedding_id`）→ 当前不返回
- `explanation` → 已返回 ✓

**修复建议：**

```go
type SampleUploadResponse struct {
    SensitiveFileID     string            `json:"sensitive_file_id"`
    FileName            string            `json:"file_name"`
    SensitiveType       string            `json:"sensitive_type"`
    RiskLevel           string            `json:"risk_level"`
    RuleVersion         int               `json:"rule_version"`
    GeneratedRulesCount int               `json:"generated_rules_count"`
    GeneratedRules      []RuleSummary     `json:"generated_rules"`      // 新增
    Fingerprint         map[string]any    `json:"fingerprint"`
    EmbeddingID         string            `json:"embedding_id"`         // 新增
    SemanticLabels      []string          `json:"semantic_labels"`      // 新增
    Explanation         string            `json:"explanation"`
}

type RuleSummary struct {
    Type          string   `json:"type"`
    RuleName      string   `json:"rule_name"`
    Keywords      []string `json:"keywords,omitempty"`
    Pattern       string   `json:"pattern,omitempty"`
    RiskLevel     string   `json:"risk_level"`
}
```

---

#### 3.9 语义标签未同步到客户端

**文件：** `server/router/rules.go:38-57`、`server/router/rules.go:212-271`

**问题：** 服务端已保存语义标签到 `semantic_features` 表，但 `GET /api/client/rules` 响应仅包含 `rules` 和 `fingerprints`，没有 `semantic_features`。客户端无法利用语义标签进行展示或辅助判断。

**修复建议：**

1. 规则同步响应增加 `semantic_labels` 字段：

```go
type RuleSyncResponse struct {
    LatestVersion   int              `json:"latest_version"`
    FullSync        bool             `json:"full_sync"`
    Rules           []RuleResp       `json:"rules"`
    Fingerprints    []FingerResp     `json:"fingerprints"`
    SemanticLabels  []SemanticResp   `json:"semantic_labels"`   // 新增
}

type SemanticResp struct {
    SensitiveFileID string   `json:"sensitive_file_id"`
    SemanticLabels  []string `json:"semantic_labels"`
    SensitiveType   string   `json:"sensitive_type"`
    RiskLevel       string   `json:"risk_level"`
}
```

2. 客户端 SQLite 新增 `cached_semantic_labels` 表，`sync.py` 同步写入。

3. 扫描结果 `match_detail` 中记录匹配到的语义标签。

---

#### 3.10 组合规则只硬编码一条

**文件：** `server/core/rule_generator.go:98-112`

**问题：** 规划 4.3.3 定义的组合规则应通过对文本内容的分析动态生成。当前仅硬编码一条"客户报价组合识别"规则（检测"客户/报价/合同金额/联系人" + "万元"金额）。

缺少的组合规则类别（规划隐含要求）：
- 财务数据组合（财务关键词 + 金额数字 + 百分比）
- 薪资信息组合（薪资关键词 + 姓名 + 身份证号/工号）
- 源代码泄露组合（代码关键字 + 账号/密码/连接串）
- 合同信息组合（合同关键词 + 日期 + 金额 + 甲乙方）

**修复建议：**

```go
// 定义组合规则模板
var combinedRuleTemplates = []struct {
    Name       string
    Keywords   []string
    Regexes    []string
    MinKWHits  int
    RiskLevel  string
}{
    {
        Name:      "财务数据组合识别",
        Keywords:  []string{"财务", "预算", "成本", "利润", "收入", "支出"},
        Regexes:   []string{`\d+(\.\d+)?万元`, `\d+(\.\d+)?%`},
        MinKWHits: 2,
        RiskLevel: "high",
    },
    {
        Name:      "薪资信息组合识别",
        Keywords:  []string{"薪资", "工资", "奖金", "绩效", "社保"},
        Regexes:   []string{`\b\d{17}[\dXx]\b`, `[一-龥]{2,4}`},
        MinKWHits: 2,
        RiskLevel: "high",
    },
    // ... 更多模板
}

func GenerateCombinedRules(text string) []RuleData {
    var rules []RuleData
    for _, tmpl := range combinedRuleTemplates {
        kwHits := countKeywordHits(text, tmpl.Keywords)
        reHits := countRegexHits(text, tmpl.Regexes)
        if kwHits >= tmpl.MinKWHits && reHits > 0 {
            rules = append(rules, RuleData{
                RuleName: tmpl.Name,
                RuleType: "combined",
                // ...
            })
        }
    }
    return rules
}
```

---

### 🟡 P2 — 工程健壮性与可扩展性

#### 3.11 环境变量命名与规划文档不一致

**文件：** `server/core/semantic.go:31-37`、`.env`、`MODULE_ONE_PLAN.md:279-283`

**问题：**

| 来源 | ChatModel 变量 | Embedding 变量 |
|---|---|---|
| `MODULE_ONE_PLAN.md` | `ARK_CHAT_MODEL` | `ARK_EMBEDDING_MODEL` |
| 代码 `semantic.go` | `ARK_ENDPOINT_ID` | 未定义 |
| `.env` 文件 | `ARK_CHAT_MODEL` | `ARK_EMBEDDING_MODEL` |

三方命名各不相同。`.env` 中设置的 `ARK_CHAT_MODEL` 和 `ARK_EMBEDDING_MODEL` 代码完全不读取，导致用户按文档配置后大模型无法启动。

**修复建议：**

统一方案（推荐修改代码兼容文档）：

```go
const (
    EnvArkAPIKey          = "ARK_API_KEY"
    EnvArkBaseURL         = "ARK_BASE_URL"
    EnvArkChatEndpointID  = "ARK_CHAT_MODEL"      // 与规划一致
    EnvArkEmbedEndpointID = "ARK_EMBEDDING_MODEL" // 与规划一致
    DefaultArkURL         = "https://ark.cn-beijing.volces.com/api/v3"
)
```

并在 `.env` 中添加注释说明三者关系。

---

#### 3.12 ZIP 上传缺少安全限制

**文件：** `server/router/rules.go:426-448`

**问题：** `parseZip` 没有实现规划 5.5 中提到的各项限制：
- 无总解压大小限制
- 无单文件大小限制
- 无文件数量上限
- 未做目录穿越检查（`../` 路径）
- 无嵌套 ZIP 层级限制

**修复建议：**

```go
const (
    maxZipTotalSize  = 100 * 1024 * 1024  // 100MB
    maxZipFileSize   = 20 * 1024 * 1024   // 20MB per file
    maxZipFileCount  = 100
)

func parseZip(data []byte) (map[string][]byte, error) {
    if len(data) > maxZipTotalSize {
        return nil, fmt.Errorf("ZIP 文件过大: %d bytes", len(data))
    }
    zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
    if err != nil {
        return nil, err
    }
    if len(zipReader.File) > maxZipFileCount {
        return nil, fmt.Errorf("ZIP 文件数量过多: %d", len(zipReader.File))
    }
    files := make(map[string][]byte)
    for _, f := range zipReader.File {
        // 安全检查：拒绝目录穿越
        cleanName := filepath.Clean(f.Name)
        if strings.HasPrefix(cleanName, "..") || filepath.IsAbs(cleanName) {
            zap.L().Warn("跳过可疑 ZIP 路径", zap.String("name", f.Name))
            continue
        }
        if f.FileInfo().IsDir() {
            continue
        }
        if f.UncompressedSize64 > maxZipFileSize {
            zap.L().Warn("跳过过大 ZIP 内部文件", zap.String("name", f.Name))
            continue
        }
        // ... 读取文件
    }
    return files, nil
}
```

---

#### 3.13 服务端指纹同步始终返回全量

**文件：** `server/router/rules.go:239-240`

**问题：** `SyncRules` 中指纹查询 `dal.DB.Find(&fingerprints)` 不带任何版本过滤，每次同步都返回所有指纹。随着样本积累，这会越来越大。

**修复建议：**

为 `FileFingerprint` 增加 `version` 字段，与 `GeneratedRule` 一起做增量同步：

```go
type FileFingerprint struct {
    SampleID   string `gorm:"primaryKey;size:64"`
    SHA256     string `gorm:"size:64;index"`
    SimHash    string `gorm:"size:32"`
    TextLength int
    Version    int    `gorm:"index"` // 新增
}
```

同步时按版本过滤：`dal.DB.Where("version > ?", clientVersion).Find(&fingerprints)`

---

#### 3.14 客户端未使用规划中的 jieba 分词库

**文件：** `client/matcher.py:205-223`、`client/requirements.txt`

**问题：** `requirements.txt` 列出了 `jieba==0.42.1` 和 `simhash==2.1.2`，但代码中：
- 未 `import jieba`，SimHash 分词用自己的简单实现
- 未 `import simhash`，SimHash 计算也用自己实现（为了与服务端 FNV-1a 一致）
- `simhash` 和 `jieba` 依赖实际未使用，但仍在 requirements.txt 中
- 规划 3.3 节要求客户端使用 `jieba` 做中文分词和 SimHash

**修复建议：**

选项 A：如果坚持客户端/服务端 SimHash 算法一致（FNV-1a），则：
- 从 `requirements.txt` 移除 `simhash==2.1.2`
- 保留 `jieba`，在关键词匹配时使用 jieba 分词提升匹配精度

选项 B：如果使用标准 SimHash 库，则需同步修改服务端算法。

推荐选项 A，并在关键词匹配中使用 jieba：

```python
import jieba

def match_keyword(text: str, rules: list) -> list:
    # 对文本分词，支持部分匹配
    tokens = set(jieba.lcut(text))
    hits = []
    for rule in rules:
        if rule.get("rule_type") != "keyword":
            continue
        keywords = rule.get("content", {}).get("keywords", [])
        min_hits = rule.get("content", {}).get("min_hits", 1)
        # 精确匹配 + 子串匹配
        matched = [kw for kw in keywords if kw in text or any(kw in t or t in kw for t in tokens)]
        if len(matched) >= min_hits:
            hits.append({...})
    return hits
```

---

#### 3.15 缺少 `core/matcher.go`

**文件：** 按照规划 7 节目录结构应存在 `server/core/matcher.go`

**问题：** 规划目录结构列出了 `core/matcher.go`，但当前代码库中不存在此文件。正则匹配逻辑内嵌在 `rule_generator.go` 中，文本匹配逻辑分散在客户端。

如果 `matcher.go` 的职责是服务端内容扫描（如 `POST /api/server/content-scan`），那当前实现是在 `router/rules.go:662-687` 的 `ContentScan` 函数中内联完成的。

**修复建议：** 将 `ContentScan` 的匹配逻辑抽取到 `server/core/matcher.go`，与其他核心逻辑保持一致的分层结构。

---

#### 3.16 `.env` 文件包含真实 API Key 且有提交风险

**文件：** `.env` (项目根目录)

**问题：** `.env` 文件中包含真实的火山方舟 API Key 和 Endpoint ID。虽然 `.gitignore` 中已列出 `.env`，但如果在添加 `.gitignore` 之前已经提交过，或有人强制执行 `git add -f .env`，密钥就会泄露。

**修复建议：**

1. 确认 `.env` 未被 Git 追踪：

```bash
git ls-files .env  # 应无输出
git log -- .env    # 应无历史
```

2. 如果曾被追踪，需要从历史中清除并轮换密钥。
3. 创建 `.env.example` 作为模板：

```
# 火山引擎方舟配置
ARK_API_KEY=xxxxx
ARK_CHAT_MODEL=ep-xxxxxxxxxxxx-xxxxx
ARK_EMBEDDING_MODEL=ep-m-xxxxxxxxxxxx-xxxxx
ARK_BASE_URL=https://ark.cn-beijing.volces.com/api/v3

# MySQL
MYSQL_DSN=root:password@tcp(127.0.0.1:3306)/sensitive_agent?charset=utf8mb4&parseTime=True&loc=Local

# 服务监听地址
SERVER_ADDR=:8080
```

---

#### 3.17 代码中的小问题汇总

| # | 文件 | 行号 | 问题 | 建议 |
|---|---|---|---|---|
| 1 | `server/core/parser.go:48` | 48 | `regexp.MustCompile` 在函数体内每次调用都重新编译 | 提升为包级变量 `var spaceRE = regexp.MustCompile(...)` |
| 2 | `server/router/rules.go:138` | 138 | `Scan(&version)` 错误被静默吞掉（`version=0`），日志未记录 | 至少记录 Warn 日志 |
| 3 | `server/router/rules.go:376` | 376 | 批量上传每个文件独立生成版本号，导致版本号跳跃且可能冲突 | 改为批次统一版本号 |
| 4 | `client/scanner.py:26-28` | 26-28 | `from local_db import LocalDB` 失败后 fallback 到 `from client.local_db import LocalDB`，但 fallback 在脚本直接运行时路径不正确 | 统一用绝对导入并配置 `PYTHONPATH` |
| 5 | `client/scanner.py:55` | 55 | `SKIP_DIRS` 硬编码，未包含常见的 `dist`、`build`、`target`、`out`、`.mypy_cache` 等 | 扩展跳过目录列表 |
| 6 | `client/scanner.py:58` | 58 | `MAX_FILE_SIZE` 为 50MB 但对于客户端扫描可能过大 | 考虑降至 20MB 并设为可配置 |
| 7 | `server/core/semantic.go:107` | 107 | LLM prompt 使用 `fmt.Sprintf` 拼接用户输入，若文本含 `%s` 会破坏模板 | 改用 `strings.Replace` 或模板引擎 |
| 8 | `server/main.go:18-22` | 18-22 | `wrap` 函数丢弃了 Hertz 的 `context.Context`，可能导致 tracing/timeout 丢失 | 将 `ctx` 传递给 handler 或直接使用 `app.HandlerFunc` 签名 |
| 9 | `client/local_db.py:13` | 13 | `row_factory = sqlite3.Row` 后 `list_tags` 返回 `dict(row)`，但其他查询返回 `Row` 对象 | 统一返回格式 |
| 10 | `server/router/rules.go:59-68` | 59-68 | ID 生成使用 `crypto/rand` 但仅用 4-8 字节，碰撞概率较高 | 改用 UUID 或至少 16 字节随机数 |

---

#### 3.18 `requirements.txt` 与实际依赖不一致

**文件：** `client/requirements.txt`

**问题：**
- `simhash==2.1.2` — 代码未使用，自实现了 FNV-1a SimHash
- `jieba==0.42.1` — 代码未使用，用简单字符分词代替
- `watchdog==4.0.2` — 未出现在 `requirements.txt` 中，规划列为可选依赖
- `python-docx==1.1.2` — 已在 requirements.txt 中 ✓

**修复建议：** 移除未使用的 `simhash`；如按 3.14 建议启用 `jieba` 则保留，否则也移除。建议最终 requirements.txt：

```txt
requests==2.32.3
jieba==0.42.1
openpyxl==3.1.5
pypdf==4.3.1
chardet==5.2.0
python-docx==1.1.2
loguru==0.7.2
```

---

## 4. 实施优先级排序

### 第一轮（P0）— 确保演示闭环可靠（预估 4-6 小时）

| 序号 | 任务 | 涉及文件 |
|---|---|---|
| 1 | 上传接口加事务保护 | `server/router/rules.go` |
| 2 | 修复版本号并发（改为自增主键） | `server/model/rule.go`、`server/router/rules.go` |
| 3 | 修复客户端敏感分级（score >= 80 才标 sensitive） | `client/scanner.py`、`client/local_db.py`、`client/client.py` |
| 4 | 补充核心单元测试 | `server/core/*_test.go`、`client/test_*.py` |
| 5 | 端到端验证脚本（上传样本 → 同步 → 扫描 → 检查结果） | 新建 `test/e2e_test.sh` |

### 第二轮（P1）— 补齐规划识别能力（预估 8-12 小时）

| 序号 | 任务 | 涉及文件 |
|---|---|---|
| 6 | 接入 gojieba + TF-IDF 关键词抽取 | `server/core/rule_generator.go`、`server/go.mod` |
| 7 | 实现 Embedding 模型调用和存储 | `server/core/semantic.go` |
| 8 | 改进服务端 PDF 解析 | `server/core/parser.go` |
| 9 | 上传响应增加完整规则摘要和语义标签 | `server/router/rules.go` |
| 10 | 规则同步增加语义标签字段 | `server/router/rules.go`、`client/sync.py`、`client/local_db.py` |
| 11 | 动态生成多种组合规则 | `server/core/rule_generator.go` |

### 第三轮（P2）— 工程健壮性提升（预估 4-6 小时）

| 序号 | 任务 | 涉及文件 |
|---|---|---|
| 12 | 统一环境变量命名（代码 + 文档 + .env.example） | `server/core/semantic.go`、`.env.example`、`MODULE_ONE_PLAN.md` |
| 13 | ZIP 上传安全限制 | `server/router/rules.go` |
| 14 | 指纹增量同步 | `server/model/fingerprint.go`、`server/router/rules.go` |
| 15 | 客户端启用 jieba 分词 | `client/matcher.py`、`client/requirements.txt` |
| 16 | 创建 `core/matcher.go` 抽取匹配逻辑 | `server/core/matcher.go`（新建） |
| 17 | 代码小问题批量修复 | 见 3.17 节表格 |
| 18 | 创建 `.env.example` | 新建文件 |

---

## 5. 已有实现的亮点

在审查过程中也发现以下值得肯定的实现：

1. **SimHash 算法一致性**：服务端 (Go FNV-1a) 和客户端 (Python 自实现 FNV-1a) 的 SimHash 算法完全一致，并通过单元测试验证了 3 个 case，确保跨语言指纹互认。

2. **大模型降级方案**：当火山方舟未配置或调用失败时，`AnalyzeSemantic` 自动降级到 `analyzeWithRules`，保证基本功能不受影响。

3. **灵活的扫描范围**：`scan_directory` 同时支持目录和单文件扫描，通过 `iter_files` 的 `root.is_file()` 检查优雅处理。

4. **客户端导入容错**：`scanner.py`、`sync.py`、`client.py` 均处理了直接运行和模块导入两种场景的 import 路径。

5. **.env 自动查找**：`main.go` 的 `findEnvFile()` 从当前目录向上查找 `.env`，方便在不同目录启动服务。

6. **环境变量宽松配置**：`MYSQL_DSN`、`SERVER_ADDR` 等均有默认值，降低了首次启动门槛。

---

## 6. 验证记录

已执行以下基础验证：

```bash
# 服务端编译 + 测试
cd SCU-project-model-1/server && go build ./... && go test ./...
# 结果：编译通过，测试通过

# 客户端语法检查
cd SCU-project-model-1/client && python -m py_compile client.py sync.py scanner.py matcher.py local_db.py
# 结果：语法检查通过
```

> 注：未启动 MySQL/Hertz 服务进行端到端验证，本文档结论基于静态代码审查和基础编译/测试结果。
