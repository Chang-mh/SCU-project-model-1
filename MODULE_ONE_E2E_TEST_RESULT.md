# 模块一完整 E2E 自动化测试结果解析与需求覆盖核对

测试时间：2026-06-15 11:15 左右  
测试脚本：`test/e2e_module_one_full.sh`  
对照文档：

- `MODULE_ONE_PLAN.md`
- `dlpagent.md` 中“模块一：敏感文件识别 Agent”部分

## 1. 测试结论

本次完整自动化 E2E 测试最终结果：

```text
Module One Full E2E PASSED (30 checks)
```

说明当前代码库已经跑通模块一的核心闭环：

```text
服务端规则接口可访问
  ↓
上传敏感样本
  ↓
生成规则、指纹、版本、语义标签
  ↓
客户端同步规则库
  ↓
客户端扫描指定目录
  ↓
SHA-256 / 规则 / 语义标签 / ZIP / 递归目录命中
  ↓
普通文件不误报
  ↓
SQLite 本地标签库写入
  ↓
重复扫描 upsert 正常
  ↓
扫描结果上报服务端
  ↓
服务端可查询上报结果
```

从模块一验收角度看，本次测试结果证明：**当前代码库已经满足 `MODULE_ONE_PLAN.md` 与 `dlpagent.md` 模块一的主要功能要求，模块一 MVP 闭环已成立。**

---

## 2. 测试环境说明

脚本开始时输出：

```text
[INFO] http://127.0.0.1:8080 is unreachable from this Bash environment; using host URL http://172.20.0.1:8080 instead.
```

这说明测试是在 PowerShell 中调用 Bash 执行，Bash/WSL 环境中的 `127.0.0.1` 与 Windows 服务端的 `127.0.0.1` 隔离。脚本已自动识别该情况，并切换到 Windows Host 地址：

```text
http://172.20.0.1:8080
```

后续所有 API 测试均通过，说明该网络切换有效。

使用的客户端 Python：

```text
/mnt/d/学习/暑期实训/大三/project/model-1/SCU-project-model-1/client/.venv/Scripts/python.exe
```

临时测试目录：

```text
.e2e_tmp/scu-model-1-e2e-20260615111514-11
```

---

## 3. 30 个检查项结果解析

### 3.1 Python 客户端依赖检查

输出：

```text
[PASS] Python client dependencies are importable
```

含义：客户端所需依赖可正常导入，包括：

- `requests`
- `loguru`
- `chardet`
- `jieba`
- `openpyxl`
- `pypdf`
- `docx`

结论：客户端运行环境满足扫描、同步和文本提取的基本要求。

---

### 3.2 测试样本准备

输出：

```text
[PASS] Prepared E2E sample files
```

脚本自动创建了以下测试文件：

```text
scan_target/
├── customer_copy.txt          # 与上传样本完全相同
├── customer_modified.txt      # 修改版客户报价敏感文件
├── secret_config.py           # API Key / password / JDBC / 内网 IP
├── normal.txt                 # 普通文件，用于误报测试
├── sensitive.zip              # ZIP 内含敏感文件
└── nested/
    └── nested_customer.txt    # 子目录中的敏感文件
```

结论：测试数据覆盖了完全一致文件、相似敏感文件、代码/配置敏感信息、普通文件、ZIP、递归目录等关键场景。

---

### 3.3 TC01：服务端规则接口连通性

输出：

```text
[PASS] GET /api/client/rules health returned HTTP 200
[PASS] rules endpoint response has required fields
```

测试接口：

```http
GET /api/client/rules?version=0
```

断言内容：

- HTTP 200；
- 返回 JSON；
- 包含 `latest_version`；
- 包含 `rules`；
- 包含 `fingerprints`；
- 包含 `config`。

结论：服务端规则同步接口可用，满足客户端规则同步的前置条件。

---

### 3.4 TC02：上传敏感样本

输出：

```text
[PASS] POST /api/server/samples returned HTTP 200
[PASS] upload response contains rules, version and fingerprints
```

测试接口：

```http
POST /api/server/samples
```

上传样本包含：

```text
客户名称
联系人
手机号
邮箱
报价
合同金额
API_KEY
```

断言内容：

- `sensitive_file_id` 非空；
- `rule_version >= 1`；
- `generated_rules_count > 0`；
- `fingerprint.sha256` 非空；
- `fingerprint.simhash` 非空；
- `generated_rules` 中存在：
  - `regex`
  - `keyword`
  - `combined`

结论：服务端样本上传、文本解析、规则生成、指纹生成、版本生成均已跑通。

---

### 3.5 TC03：重复上传去重

输出：

```text
[PASS] duplicate POST /api/server/samples returned HTTP 409
[PASS] duplicate response contains duplicate details
```

测试行为：重复上传同一个样本文件。

预期：服务端通过 SHA-256 判断该文件已经存在，返回 `409 Conflict`。

结论：样本去重能力正常，可避免敏感文件库重复写入相同文件。

---

### 3.6 TC04：根据 SHA-256 查询敏感文件

输出：

```text
[PASS] GET /api/server/sensitive-files/{sha256} returned HTTP 200
[PASS] sensitive file query returns uploaded sample
```

测试接口：

```http
GET /api/server/sensitive-files/{sha256}
```

结论：服务端可根据文件 hash 查询敏感样本信息。该能力可为后续模块二外发监控提供“某文件是否敏感”的查询基础。

---

### 3.7 TC05：规则同步接口内容检查

输出：

```text
[PASS] GET /api/client/rules after upload returned HTTP 200
[PASS] rules sync response contains executable rules and config
```

断言内容：

- `latest_version >= 1`；
- `rules` 非空；
- `fingerprints` 非空；
- 同步规则中存在：
  - `regex`
  - `keyword`
  - `combined`
- `config.simhash_threshold` 存在；
- `config.semantic_label_hints` 存在。

结论：服务端规则库、指纹库和客户端配置均可下发，满足客户端本地扫描前置条件。

---

### 3.8 TC06：客户端同步规则并写入 SQLite

输出：

```text
[PASS] client sync command succeeds
[PASS] SQLite DB file created
[PASS] SQLite rule cache contains rules, fingerprints and version
```

客户端命令：

```bash
python client.py --db <临时DB> sync --server http://172.20.0.1:8080
```

日志显示：

```text
规则同步成功: 新增45条规则, 3条指纹, 3条语义标签, 版本=3
```

说明：

- 客户端成功拉取服务端规则；
- 本地 SQLite 数据库创建成功；
- `cached_rules` 非空；
- `cached_fingerprints` 非空；
- `local_rules_version` 记录了版本；
- `cached_semantic_labels` 已同步语义标签。

结论：客户端规则同步和本地缓存功能正常。

---

### 3.9 TC07：客户端扫描目录

输出：

```text
[PASS] scan output is non-empty JSON array
[PASS] exact copy hits SHA-256 with score 100
[PASS] modified customer file is recognized as non-clean
[PASS] secret config file is recognized by regex rules
[PASS] normal file stays clean
[PASS] nested directory file is scanned
[PASS] zip inner file is scanned
```

本阶段是模块一最核心的客户端扫描能力测试。

#### 3.9.1 完全相同文件命中 SHA-256

文件：

```text
customer_copy.txt
```

输出日志：

```text
confidence=sensitive, score=100, risk=high
```

断言：

- `sensitive = true`；
- `confidence_level = sensitive`；
- `match_score = 100`；
- `match_detail.sha256_hit = true`。

结论：完全相同敏感文件可以通过 SHA-256 精确命中。

#### 3.9.2 修改版客户文件被识别

文件：

```text
customer_modified.txt
```

输出日志：

```text
confidence=sensitive, score=100, risk=high
```

说明修改版文件虽然与样本不完全相同，但包含客户名称、手机号、邮箱、报价、合同金额等信息，因此通过正则、关键词、组合规则和/或语义标签达到敏感判定。

结论：相似或改写后的客户敏感文件可以被识别。

#### 3.9.3 密钥配置文件被识别

文件：

```text
secret_config.py
```

内容包含：

- `API_KEY`
- `password`
- `jdbc:mysql`
- `192.168.1.10`

输出日志：

```text
confidence=sensitive, score=100, risk=high
```

断言还检查了 `regex_hits` 非空。

结论：代码/配置文件中的密钥、密码、数据库连接串、内网 IP 等固定格式敏感信息识别正常。

#### 3.9.4 普通文件保持 clean

文件：

```text
normal.txt
```

输出日志：

```text
score=0
```

断言：

- `sensitive = false`；
- `confidence_level = clean`；
- `match_score < 30`。

结论：普通文件未被误标为敏感，基础误报控制通过。

#### 3.9.5 子目录递归扫描

文件：

```text
nested/nested_customer.txt
```

输出日志：

```text
confidence=sensitive, score=100, risk=high
```

结论：客户端支持递归扫描指定目录下的子目录。

#### 3.9.6 ZIP 内文件扫描

文件：

```text
sensitive.zip!zip_customer.txt
```

输出日志：

```text
confidence=sensitive, score=100, risk=high
```

结论：客户端 ZIP 内文件扫描能力正常。ZIP 扫描属于当前实现的增强能力，已被自动化测试覆盖。

---

### 3.10 TC08：SQLite 本地标签库和 list 命令

输出：

```text
[PASS] SQLite local_file_tags contains 6 rows
[PASS] client list --sensitive-only returns sensitive rows
```

说明：扫描目录中共写入 6 条本地标签记录，对应：

1. `customer_copy.txt`
2. `customer_modified.txt`
3. `normal.txt`
4. `secret_config.py`
5. `sensitive.zip!zip_customer.txt`
6. `nested/nested_customer.txt`

其中敏感文件可通过：

```bash
python client.py list --sensitive-only --json
```

查询到。

结论：模块一要求的本地 SQLite 标签库能力已验证通过。

---

### 3.11 TC09：重复扫描 upsert 检查

输出：

```text
[PASS] Repeated scan preserved local_file_tags row count (6)
```

测试行为：对同一目录重复扫描。

断言：第二次扫描后 `local_file_tags` 记录数仍为 6。

结论：客户端对同一 `(file_path, file_hash)` 使用 upsert 更新，不会重复插入相同文件标签。

---

### 3.12 TC10：扫描结果上报服务端

输出：

```text
[PASS] POST /api/client/scan-results returned HTTP 200
[PASS] scan report response accepted
```

测试接口：

```http
POST /api/client/scan-results
```

断言：

- HTTP 200；
- 返回 `status = received`；
- 返回 `report_id`；
- `received > 0`。

结论：客户端扫描结果可以上报服务端，模块一为后续模块二/三/四复用扫描结果提供了接口基础。

---

### 3.13 TC11：查询服务端扫描结果

输出：

```text
[PASS] GET /api/server/scan-results returned HTTP 200
[PASS] reported scan results are queryable
```

测试接口：

```http
GET /api/server/scan-results
```

断言：

- HTTP 200；
- `total > 0`；
- `results` 非空。

结论：服务端可以查询客户端上报的扫描结果。

---

### 3.14 TC12：不存在路径负向测试

输出：

```text
[PASS] Scanning a nonexistent path fails as expected
```

测试行为：扫描一个不存在的路径。

结论：客户端对非法扫描路径会失败退出，而不是静默成功或写入错误数据。

---

## 4. 与 `MODULE_ONE_PLAN.md` 的覆盖性对照

| `MODULE_ONE_PLAN.md` 要求 | 本次测试覆盖情况 | 结论 |
|---|---|---|
| 样本文件上传 | TC02 上传客户样本 | 已覆盖 |
| 文本抽取 | 上传 txt 样本并扫描 txt / py / zip 内 txt | 已覆盖核心文本类型 |
| 敏感规则生成 | TC02 / TC05 检查 regex、keyword、combined | 已覆盖 |
| 文件 SHA-256 指纹生成 | TC02 检查 `fingerprint.sha256`，TC07 检查 SHA-256 命中 | 已覆盖 |
| SimHash 指纹生成 | TC02 检查 `fingerprint.simhash`，TC05 检查同步指纹 | 已覆盖生成与同步；未单独强制断言 simhash_hit |
| 大模型语义识别 / 降级语义标签 | TC06 同步 3 条语义标签，TC05 检查 `semantic_label_hints` | 已覆盖语义标签同步；未强制验证 LLM 内容正确性 |
| 服务端敏感文件库管理 | TC02 入库，TC04 hash 查询，TC03 去重 | 已覆盖 |
| 规则版本管理 | TC02 检查 `rule_version`，TC05/TC06 检查版本同步 | 已覆盖 |
| 客户端规则同步 | TC06 `client.py sync` | 已覆盖 |
| 客户端指定目录扫描 | TC07 扫描整个目录 | 已覆盖 |
| 递归扫描 | TC07 子目录 `nested_customer.txt` | 已覆盖增强场景 |
| 敏感文件识别与打标 | TC07 识别，TC08 写入 SQLite | 已覆盖 |
| 本地 SQLite 标签库 | TC08 / TC09 | 已覆盖 |
| 扫描结果本地查看 | TC08 `list --sensitive-only --json` | 已覆盖 |
| 扫描结果上报服务端 | TC10 | 已覆盖预留接口 |
| 服务端查看扫描结果 | TC11 | 已覆盖增强接口 |
| 普通文件不误报 | TC07 `normal.txt` clean | 已覆盖 |
| 重复扫描不重复插入 | TC09 | 已覆盖 |
| 不存在路径处理 | TC12 | 已覆盖负向场景 |

结论：本次 E2E 已覆盖 `MODULE_ONE_PLAN.md` 中模块一核心闭环和主要验收点。

---

## 5. 与 `dlpagent.md` 模块一要求的覆盖性对照

`dlpagent.md` 中模块一要求集中在第三章，核心包括：

1. 服务端上传样本敏感文件，构建敏感文件库；
2. 客户端同步敏感文件库；
3. 客户端识别指定目录下的敏感文件；
4. 正则表达式规则；
5. 关键词规则；
6. 文本特征指纹；
7. 向量化特征，可选；
8. 固定格式敏感信息识别；
9. 企业业务敏感信息识别；
10. 文档语义特征识别；
11. 输出 `sensitive_file_id`、规则、指纹、embedding、explanation；
12. 客户端文本提取能力；
13. 敏感文件标记。

逐项对照如下：

| `dlpagent.md` 模块一要求 | 本次测试覆盖情况 | 结论 |
|---|---|---|
| 服务端上传样本敏感文件 | TC02 | 已覆盖 |
| 构建敏感文件库 | TC02 入库，TC04 hash 查询 | 已覆盖 |
| 客户端同步敏感文件库 | TC06 | 已覆盖 |
| 客户端识别指定目录下敏感文件 | TC07 | 已覆盖 |
| 正则表达式规则 | TC02/TC05 检查规则，TC07 `secret_config.py` regex 命中 | 已覆盖 |
| 关键词规则 | TC02/TC05 检查 keyword 规则，客户文件识别 | 已覆盖 |
| 组合规则 | TC02/TC05 检查 combined 规则，客户报价文件识别 | 已覆盖 |
| 文本特征指纹 | TC02 指纹生成，TC07 SHA-256 命中 | 已覆盖 |
| SimHash | TC02 检查生成，TC05 检查同步指纹 | 已覆盖生成和下发；未单独断言相似命中一定来自 SimHash |
| 向量化特征可选 | 当前测试未强制验证 embedding 向量内容 | 可选能力，不作为失败项 |
| 固定格式敏感信息：手机号、邮箱、API Key、密码、连接串、内网 IP | TC07 `customer_modified.txt` / `secret_config.py` | 已覆盖代表性固定格式 |
| 企业业务敏感信息：客户名称、报价、合同金额 | TC02 / TC07 客户资料文件 | 已覆盖代表性业务敏感信息 |
| 文档语义特征 | TC05/TC06 检查 `semantic_label_hints` 与语义标签同步 | 已覆盖同步链路；未评价 LLM 语义质量 |
| 输出 `sensitive_file_id` | TC02 / TC04 | 已覆盖 |
| 输出规则 | TC02 / TC05 | 已覆盖 |
| 输出指纹 | TC02 | 已覆盖 |
| 输出 embedding / explanation | TC02 检查响应结构的一部分；未强制 embedding 非空 | embedding 属可选，LLM/配置相关 |
| 客户端文本提取能力 | TC07 覆盖 txt、py、zip 内 txt | 已覆盖核心文本提取 |
| 敏感文件标记 | TC08 SQLite `local_file_tags` | 已覆盖 |

结论：本次 E2E 对 `dlpagent.md` 模块一的核心要求覆盖充分。可选能力如完整 embedding 向量质量、PDF/OCR、老版 Office、图片、邮件等不属于本次核心验收失败条件。

---

## 6. 已覆盖的文件类型与场景

| 类型 / 场景 | 测试文件 | 结果 |
|---|---|---|
| txt 样本上传 | `customer_*.txt` | 通过 |
| txt 完全相同文件 | `customer_copy.txt` | SHA-256 命中，score=100 |
| txt 相似敏感文件 | `customer_modified.txt` | sensitive，score=100 |
| Python/配置敏感文件 | `secret_config.py` | regex 命中，score=100 |
| 普通 txt 文件 | `normal.txt` | clean，score=0 |
| 子目录文件 | `nested/nested_customer.txt` | sensitive，score=100 |
| ZIP 内 txt 文件 | `sensitive.zip!zip_customer.txt` | sensitive，score=100 |
| 不存在路径 | `does-not-exist` | 失败退出，符合预期 |

---

## 7. 未完全覆盖但不影响模块一核心验收的内容

以下能力属于可选增强或更细粒度测试，本次 E2E 未作为强制验收项：

| 能力 | 原因 | 建议 |
|---|---|---|
| docx 文本提取 | 当前脚本优先覆盖稳定文本类文件 | 可在 extended 脚本中生成 docx 样本 |
| xlsx 文本提取 | 当前脚本未生成 xlsx | 可用 openpyxl 生成薪资/报价表样本 |
| PDF 文本提取 | PDF 环境差异较大，扫描件依赖 OCR | 可作为增强测试 |
| OCR / 图片识别 | `MODULE_ONE_PLAN.md` 标记为后续增强 | 不纳入核心验收 |
| 老版 doc/xls、ppt/pptx、eml/msg、rar/7z | 规划中为可选增强 | 不纳入核心验收 |
| embedding 向量内容正确性 | 向量化为可选，且依赖外部模型配置 | 可增加“配置 embedding 后检查 embedding_id 非空/embedding 入库”的专项测试 |
| LLM 语义分类准确性 | 语义质量依赖模型输出，自动断言难稳定 | 可增加人工样例或 mock 模型测试 |
| SimHash 相似命中来源强断言 | 相似文件可能同时被正则/关键词/组合规则命中 | 如需可构造专门只改动少量内容且无额外规则命中的样本 |

---

## 8. 本次测试发现的注意点

### 8.1 WSL / Bash 与 Windows localhost 隔离

脚本一开始自动切换了服务地址：

```text
http://127.0.0.1:8080 → http://172.20.0.1:8080
```

说明当前运行环境中 Bash 的 `127.0.0.1` 不能直接访问 Windows Go 服务端。脚本已自动兼容该情况。

### 8.2 客户端同步日志显示版本相同但仍返回规则

日志中有：

```text
本地版本=3, 请求=http://172.20.0.1:8080/api/client/rules?version=3
规则同步成功: 新增19条规则, 0条指纹, 0条语义标签, 版本=3
```

这说明当客户端本地版本等于服务端版本时，服务端仍返回了一部分规则，或客户端仍执行了保存逻辑。当前不影响功能，因为扫描和标签写入均通过；后续如果要优化增量同步效率，可以检查 `SyncRules` 对 `version` 的返回策略。

### 8.3 多次 E2E 会向 MySQL 写入新样本和扫描报告

脚本每次都会生成带 `RUN_ID` 的唯一样本，因此不会与历史样本冲突。但多次运行会在服务端数据库中留下测试样本和扫描报告。若需要清理，可后续增加专门的测试数据清理脚本或在数据库中按 `E2E` 描述字段筛选删除。

---

## 9. 总体评价

本次自动化测试结果证明：

1. 服务端样本上传能力正常；
2. 服务端可以生成正则、关键词、组合规则；
3. 服务端可以生成 SHA-256 和 SimHash 指纹；
4. 服务端可以维护规则版本并下发规则；
5. 客户端可以同步规则库；
6. 客户端可以扫描指定目录；
7. 客户端可以识别完全相同、相似、包含固定格式敏感信息的文件；
8. 客户端可以避免普通文件误报；
9. 客户端可以写入本地 SQLite 标签库；
10. 客户端重复扫描不会重复插入标签；
11. 客户端扫描结果可以上报服务端；
12. 服务端可以查询上报的扫描结果。

因此，本轮结论为：

> 当前代码库已经通过模块一完整 E2E 自动化测试，测试覆盖了 `MODULE_ONE_PLAN.md` 和 `dlpagent.md` 中模块一的主要功能要求。模块一 MVP 闭环可验收。后续工作可聚焦在 docx/xlsx/pdf/OCR/embedding 质量等增强测试，以及测试数据清理和增量同步效率优化。
