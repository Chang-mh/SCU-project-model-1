# 模块一实现对照 MODULE_ONE_PLAN.md 的后续修改建议

审查对象：当前 `SCU-project-model-1` 代码库  
对照文档：`MODULE_ONE_PLAN.md`  
审查结论：当前代码已经基本覆盖模块一 MVP 主链路（样本上传 → 文本解析 → 规则/指纹/语义特征生成 → MySQL 持久化 → 客户端同步 → 本地扫描 → SQLite 标记），但仍有若干功能性、演示可靠性和安全性问题建议继续修复。

---

## 1. 总体结论

当前实现已基本满足 `MODULE_ONE_PLAN.md` 中模块一的核心闭环：

- 服务端已实现样本上传、批量上传、ZIP 上传、文本解析、规则生成、指纹生成、语义识别、Embedding 存储、规则版本管理和客户端同步 API。
- 客户端已实现规则同步、目录/文件扫描、SHA-256/SimHash/正则/关键词/组合规则匹配、敏感/疑似/低置信分级和 SQLite 本地标签库。
- 已补充多项单元测试和一个可选 E2E smoke test。

但如果严格对照规划书和演示稳定性，仍建议修复以下问题。

---

## 2. 高优先级问题

### 2.1 服务端敏感接口无认证，且列表接口可能泄露完整提取文本

**涉及文件：**

- `server/main.go`
- `server/router/rules.go`
- `server/model/sample.go`

**问题：**

当前所有上传、查询、同步、列表接口均未做认证。并且部分列表接口直接返回 `model.SensitiveSample`，该结构中包含 `ExtractedText` 字段。敏感样本的提取文本可能被任意调用者读取。

**影响：**

- 如果服务端绑定到非本机地址或在共享网络中演示，上传的敏感样本文本可能泄露。
- 与“敏感文件识别”系统的安全预期不符。

**建议：**

1. 增加简单 API Key / Bearer Token 中间件。
2. 默认服务监听地址建议改为 `127.0.0.1:8080`，除非显式配置公网/局域网地址。
3. 列表接口返回 DTO，不直接返回 `SensitiveSample` 模型。
4. 默认隐藏 `ExtractedText`，仅在受控详情接口中返回。

---

### 2.2 GORM SQL 日志可能输出敏感样本文本

**涉及文件：**

- `server/dal/mysql.go`

**问题：**

当前 GORM 使用 Info 日志级别，上传样本时 `ExtractedText` 会被写入数据库。如果 SQL 日志输出绑定参数，可能把完整敏感文本写入控制台、日志文件或 CI 输出。

**影响：**

- 敏感内容进入日志，形成二次泄露。
- 演示录屏或日志收集时风险较高。

**建议：**

1. 默认将 GORM 日志级别改为 `Warn` 或 `Silent`。
2. 仅在显式设置 `GORM_LOG_LEVEL=info/debug` 时开启详细 SQL 日志。
3. 避免在日志中输出 `ExtractedText`、API Key、OCR 返回全文等敏感字段。

---

### 2.3 敏感文件查询接口空条件会误返回第一条样本

**涉及文件：**

- `server/router/rules.go`

**问题：**

`QuerySensitiveFile` 接收 `file_hash`、`file_path`、`file_name`，但实际只使用 `file_hash` 和 `file_name`。当请求体为空 `{}` 或只传 `file_path` 时，会对样本表直接执行 `First(&sample)`，导致返回第一条样本并标记为敏感。

**影响：**

- 下游模块查询时可能出现严重误报。
- 空请求也能得到敏感结果，不符合接口语义。

**建议：**

1. 至少要求 `file_hash` 或 `file_name` 中一个非空，否则返回 `400`。
2. 如果保留 `file_path` 字段，应明确实现路径匹配或从请求结构中移除。
3. 对 `file_hash` 做 SHA-256 十六进制格式校验。

---

### 2.4 上传接口仍缺少原始上传文件大小限制

**涉及文件：**

- `server/router/rules.go`

**问题：**

虽然 ZIP 解压后已经加入安全限制，但单文件上传、批量上传、指纹计算、ZIP 原始上传包本身仍通过 `io.ReadAll` 读取，缺少上传前大小限制。

**影响：**

- 超大文件可能导致服务端内存压力过高。
- 批量上传时风险叠加。
- ZIP 安全限制无法防止“上传包本身过大”的问题。

**建议：**

1. 基于 `fileHeader.Size` 增加单文件最大上传大小限制。
2. 读取时使用 `io.LimitReader` 双保险。
3. 批量上传增加总上传大小限制。
4. 超限返回 `413 Payload Too Large`。

---

### 2.5 服务端生成的部分 Go 正则无法被 Python 客户端执行

**涉及文件：**

- `server/core/rule_generator.go`
- `client/matcher.py`

**问题：**

服务端内置正则中使用了 Go regexp 支持的 `\p{Han}`，但 Python 标准库 `re` 不支持该语法。客户端捕获 `re.error` 后静默跳过规则。

典型受影响规则：

- 车牌号规则
- 地址规则

**影响：**

- `MODULE_ONE_PLAN.md` 中列出的地址、车牌等固定格式识别在客户端可能失效。
- 演示样本如果依赖这些规则，客户端扫描会漏报。

**建议：**

1. 将同步给客户端的正则改为 Python 兼容形式，例如使用 `[一-龥]`。
2. 或新增规则方言字段并在客户端同步时做 Go regexp → Python regexp 转换。
3. 增加测试：用服务端内置规则在客户端 Python `re` 中逐条编译验证。

---

## 3. 中优先级功能差距

### 3.1 指纹已增量同步，但语义特征仍非版本化

**涉及文件：**

- `server/model/fingerprint.go`
- `server/router/rules.go`
- `client/local_db.py`

**问题：**

当前 `FileFingerprint` 已增加版本号并支持增量同步，但 `SemanticFeature` 没有版本字段。服务端同步语义标签时仍倾向返回全量语义特征。

**影响：**

- 小规模 MVP 可以接受。
- 数据量增长后，同步性能下降。
- 与规划中的“版本化敏感文件库”语义不完全一致。

**建议：**

1. 为 `SemanticFeature` 增加 `Version int` 字段。
2. 保存语义特征时写入当前规则版本号。
3. 同步时按 `version > clientVersion` 返回语义标签。
4. 对历史数据可提供一次性回填脚本或说明。

---

### 3.2 规则同步在“无新增规则但有指纹/语义变化”场景下仍有边界风险

**涉及文件：**

- `server/router/rules.go`

**问题：**

当前同步主逻辑仍以 `GeneratedRule` 为核心。如果某个样本没有生成任何规则，但生成了指纹和语义标签，那么可能出现只依赖规则判断而提前返回的边界问题。

**影响：**

- 二进制文件或纯指纹样本可能无法完整同步到客户端。
- 与“样本库/指纹库也属于敏感文件库”的规划不完全一致。

**建议：**

1. 同步时不要仅以 `len(rules) == 0` 判断无需更新。
2. 分别查询 rules、fingerprints、semantic_features 三类增量。
3. 只要任一类有增量，就返回 `updated=true` 的数据。

---

### 3.3 Embedding 已实现，但未使用 Eino Embedding 组件

**涉及文件：**

- `server/core/semantic.go`

**问题：**

当前 ChatModel 通过 Eino 接入，Embedding 使用 OpenAI-compatible HTTP 直接请求 `/embeddings`。功能上可用，但与 `MODULE_ONE_PLAN.md` 中“Eino ChatModel / Embedding”描述不完全一致。

**影响：**

- 不影响 MVP 功能。
- 技术选型与规划有轻微差异。

**建议：**

1. 如果 Eino 当前版本提供稳定 Embedding 组件，可后续迁移。
2. 如果继续使用 HTTP 直连，应在 README 或方案补充中说明这是为了兼容 OpenAI-compatible 接口和降低依赖风险。

---

### 3.4 关键词抽取未使用 gojieba TF-IDF

**涉及文件：**

- `server/core/rule_generator.go`
- `server/go.mod`

**问题：**

规划建议使用 gojieba/TF-IDF。当前实现采用业务词表加权、普通 token 和中文 n-gram。该方案避免了 Windows CGO 风险，但与规划技术路线不完全一致。

**影响：**

- 功能可用，但关键词可解释性和准确性可能弱于 TF-IDF。
- n-gram 可能产生一些低价值短语。

**建议：**

1. 若演示环境支持 CGO/C++，可评估引入 gojieba。
2. 若继续保持纯 Go，应加强业务词表、停用词和低价值 n-gram 过滤。
3. 增加“普通业务文档不误报”的负样本测试。

---

### 3.5 语义标签已同步，但尚未参与语义匹配

**涉及文件：**

- `client/scanner.py`
- `client/local_db.py`

**问题：**

客户端已缓存语义标签，但目前主要在 SHA/SimHash 命中后写入 `match_detail`，没有独立的语义标签匹配或向量相似度匹配。

**影响：**

- 当前依旧主要靠指纹、正则、关键词、组合规则识别。
- 语义标签还未形成独立识别能力。

**建议：**

1. MVP 阶段可接受，仅作为解释信息。
2. 后续可增加服务端语义相似查询接口，客户端提交文本摘要/embedding 后由服务端判断。
3. 或在客户端引入轻量语义标签关键词映射规则。

---

### 3.6 SimHash 中文相似性仍较脆弱

**涉及文件：**

- `server/core/fingerprint.go`
- `client/matcher.py`

**问题：**

当前中英文 SimHash 算法已经保持服务端/客户端一致，但中文部分使用非重叠 2 字符切分。中文文本一旦插入/删除少量字符，后续 token 可能整体错位。

**影响：**

- 对轻微改写的中文敏感文件，相似识别稳定性不足。

**建议：**

1. 改为重叠 CJK bigram/trigram。
2. 或服务端和客户端统一使用 jieba 分词。
3. 改动前必须保留跨语言测试向量，避免服务端/客户端算法不一致。

---

### 3.7 文件格式覆盖仍是 MVP 水平

**涉及文件：**

- `server/core/parser.go`
- `client/scanner.py`

**当前已覆盖：**

- txt/csv/json/xml/md
- 代码/配置文本
- docx
- xlsx
- pdf（服务端 PaddleOCR API + fallback，客户端 pypdf）
- 服务端 ZIP 上传

**未覆盖或仅作为后续增强：**

- doc/xls 老版 Office
- ppt/pptx
- 图片 OCR
- 邮件 eml/msg
- 客户端压缩包递归扫描

**建议：**

按演示需求决定是否继续扩展。若演示只使用 txt/docx/xlsx/pdf/zip，当前已够用。

---

## 4. 低优先级与演示建议

### 4.1 客户端单文件扫描也应应用大小限制

**涉及文件：**

- `client/scanner.py`

**问题：**

目录扫描时会检查文件大小，但直接扫描单个文件时，应确认也应用相同限制。

**建议：**

在 `iter_files(root)` 的 `root.is_file()` 分支中加入大小判断。

---

### 4.2 本地 SQLite 数据库可能影响演示复现

**涉及文件：**

- `client/sensitive_tags.db`（本地生成文件）

**问题：**

虽然 `*.db` 已被 `.gitignore` 忽略，但本地存在旧 DB 时，可能影响演示结果。

**建议：**

演示时使用独立 DB：

```bash
python client.py --db sensitive_tags_demo.db sync --server http://127.0.0.1:8080
python client.py --db sensitive_tags_demo.db scan --path ../samples --server http://127.0.0.1:8080
```

或演示前执行：

```bash
python client.py --db sensitive_tags_demo.db clear
```

---

## 5. 建议后续修改优先级

### 第一优先级：安全与明显功能错误

1. 增加 API Key / Bearer Token 鉴权。
2. 列表接口隐藏 `ExtractedText`。
3. GORM 默认关闭 Info SQL 日志。
4. 修复 `QuerySensitiveFile` 空查询误报。
5. 增加上传文件大小限制。
6. 修复 Go regex 到 Python regex 的兼容问题。

### 第二优先级：同步和识别准确性

1. 为 `SemanticFeature` 增加版本号。
2. 同步接口拆分 rules/fingerprints/semantic_labels 增量判断，避免规则为空时漏同步。
3. 改进中文 SimHash tokenization。
4. 增强关键词抽取的可解释性和负样本测试。

### 第三优先级：规划增强能力

1. 评估是否接入 gojieba TF-IDF。
2. 评估是否迁移到 Eino Embedding 组件。
3. 增加 doc/xls/ppt/pptx/图片/邮件格式支持。
4. 增加客户端压缩包递归扫描能力。
5. 增加语义相似检索或服务端 embedding 检索接口。

---

## 6. 是否满足 MODULE_ONE_PLAN.md？

**结论：基本满足 MVP 闭环，但仍不是完整增强版。**

如果目标是课程实训/模块一 MVP 演示，当前代码已经具备完整主链路能力。  
如果目标是严格实现规划书中所有增强能力和具备较好安全性，则仍需按本文第 5 节继续修复。