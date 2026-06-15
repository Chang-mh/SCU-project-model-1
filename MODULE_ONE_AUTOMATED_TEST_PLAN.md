# 模块一自动化测试方案

本文档用于说明模块一（敏感文件识别 Agent）的自动化测试范围、测试模块、测试用例与脚本验收标准。对应需求来源：`MODULE_ONE_PLAN.md` 与 `dlpagent.md` 中“模块一：敏感文件识别 Agent”。

## 1. 测试目标

模块一的完整闭环为：

```text
服务端启动
  ↓
上传样本敏感文件
  ↓
服务端文本抽取
  ↓
生成正则规则 / 关键词规则 / 组合规则
  ↓
生成 SHA-256 / SimHash 指纹
  ↓
生成语义标签 / 可选 embedding
  ↓
规则版本入库
  ↓
客户端同步规则
  ↓
客户端扫描指定目录
  ↓
本地 SQLite 写入标签
  ↓
可选：扫描结果上报服务端
```

自动化测试的目标是验证上述链路可稳定跑通，并覆盖核心功能、负向场景和已实现的预留接口。

## 2. 测试范围总览

| 模块 | 测试内容 | 必须性 |
|---|---|---:|
| 服务端连通性 | `GET /api/client/rules?version=0` 可访问，返回 JSON | 必须 |
| 样本上传 | `POST /api/server/samples` 上传 txt 样本并返回规则、指纹、版本 | 必须 |
| 规则生成 | 验证 regex / keyword / combined 规则存在 | 必须 |
| 指纹生成 | 验证 SHA-256 和 SimHash 返回非空 | 必须 |
| 规则同步接口 | 验证 `rules`、`fingerprints`、`config` 结构 | 必须 |
| 敏感文件查询 | 通过样本 SHA-256 查询敏感文件信息 | 建议 |
| 客户端同步 | `python client.py sync` 写入本地 SQLite 缓存 | 必须 |
| 客户端扫描 | 扫描目录，识别完全相同样本、相似文本、密钥配置文件 | 必须 |
| 负向识别 | 普通文件不应被标为敏感 | 必须 |
| SQLite 标签库 | 扫描结果写入 `local_file_tags`，重复扫描不重复插入 | 必须 |
| 本地 list 命令 | `client.py list --sensitive-only --json` 可查询敏感标签 | 必须 |
| 扫描结果上报 | `POST /api/client/scan-results` 可接收客户端结果 | 建议 |
| 服务端扫描结果查询 | `GET /api/server/scan-results` 可查询上报结果 | 建议 |
| 重复上传 | 同一文件重复上传返回 409 或明确重复 | 建议 |
| ZIP 扫描 | ZIP 内敏感文件可被扫描识别 | 建议 |
| 异常路径 | 扫描不存在路径应失败退出 | 建议 |

## 3. 服务端测试模块

### 3.1 服务端连通性

测试接口：

```http
GET /api/client/rules?version=0
```

断言：

- HTTP 状态码为 200；
- 响应为 JSON；
- 包含 `latest_version`、`rules`、`fingerprints`、`config` 字段。

### 3.2 样本上传

测试接口：

```http
POST /api/server/samples
Content-Type: multipart/form-data
```

测试样本建议包含：

```text
客户名称：四川示例科技有限公司
联系人：张三
手机号：13800138000
邮箱：test@example.com
报价：50万元
API_KEY = abcdefghijklmnop123456
```

断言：

- HTTP 状态码为 200；
- `sensitive_file_id` 非空；
- `rule_version >= 1`；
- `generated_rules_count > 0`；
- `fingerprint.sha256` 非空；
- `fingerprint.simhash` 非空；
- `generated_rules` 中至少包含 `regex`、`keyword`、`combined` 规则类型。

### 3.3 固定格式敏感信息规则

建议覆盖以下固定格式规则：

| 类型 | 测试样例 | 必须性 |
|---|---|---:|
| 手机号 | `13800138000` | 必须 |
| 邮箱 | `test@example.com` | 必须 |
| API Key | `API_KEY = abcdefghijklmnop123456` | 必须 |
| 密码 | `password = SuperSecret123` | 必须 |
| 数据库连接串 | `jdbc:mysql://127.0.0.1:3306/app` | 必须 |
| 内网 IP | `192.168.1.10` | 建议 |
| 银行卡号 | 有效 Luhn 号码命中，无效长数字不命中 | 建议 |
| 身份证号 | 18 位身份证格式 | 建议 |
| 私钥 | `-----BEGIN PRIVATE KEY-----` | 建议 |

### 3.4 关键词规则

断言：

- 同步规则中存在 `rule_type = keyword`；
- `content.keywords` 非空；
- `content.min_hits` 存在。

### 3.5 组合规则

断言：

- 同步规则中存在 `rule_type = combined`；
- `content.logic` 存在；
- `content.conditions` 非空；
- 客户报价类样本可触发组合规则或至少使扫描分数进入非 clean 区间。

### 3.6 指纹能力

#### SHA-256 完全匹配

测试方式：上传样本后，扫描完全相同内容的文件。

断言：

```json
{
  "sensitive": true,
  "confidence_level": "sensitive",
  "match_score": 100,
  "match_detail": {
    "sha256_hit": true
  }
}
```

#### SimHash / 相似内容匹配

测试方式：扫描与样本相似但不完全相同的客户报价文件。

断言建议：

- `match_score >= 30`；
- `confidence_level != clean`。

不建议强制只断言 `simhash_hit = true`，因为相似文件也可能通过正则、关键词、组合规则达到识别分数。

### 3.7 敏感文件查询

测试接口：

```http
GET /api/server/sensitive-files/{sha256}
```

断言：

- 使用上传样本返回的 `fingerprint.sha256` 查询时 HTTP 200；
- 响应包含 `sensitive_file_id` 或样本基本信息。

### 3.8 扫描结果上报与查询

测试接口：

```http
POST /api/client/scan-results
GET /api/server/scan-results
```

断言：

- 上报接口返回 `status = received`；
- 返回 `report_id`；
- `received` 等于上报结果数量；
- 查询接口返回的 `total >= 1`。

## 4. 客户端测试模块

### 4.1 规则同步

测试命令：

```bash
python client.py --db "$DB_FILE" sync --server "$SERVER_URL"
```

断言：

- 命令退出码为 0；
- 输出 JSON 中 `success = true`；
- 本地 SQLite 文件存在；
- `cached_rules` 表记录数大于 0；
- `cached_fingerprints` 表记录数大于 0；
- `local_rules_version.version >= 1`。

### 4.2 目录扫描

测试命令：

```bash
python client.py --db "$DB_FILE" scan --path "$SCAN_DIR" --server "$SERVER_URL" --json
```

建议测试目录结构：

```text
tmp_e2e/
├── upload_sample/
│   └── customer.txt
└── scan_target/
    ├── customer_copy.txt          # 完全相同，测 SHA-256
    ├── customer_modified.txt      # 相似文本，测规则/SimHash
    ├── secret_config.py           # API Key / password / db connection
    ├── normal.txt                 # 普通文件，测误报
    ├── nested/
    │   └── nested_customer.txt    # 递归扫描
    └── sensitive.zip              # ZIP 内敏感文件
```

断言：

| 文件 | 预期 |
|---|---|
| `customer_copy.txt` | `score = 100`，`sensitive = true`，`sha256_hit = true` |
| `customer_modified.txt` | `score >= 30`，`confidence_level != clean` |
| `secret_config.py` | 命中高危正则，`score >= 30` |
| `normal.txt` | `score < 30`，`confidence_level = clean`，`sensitive = false` |
| `nested_customer.txt` | 被递归扫描并识别为非 clean |
| `sensitive.zip!zip_customer.txt` | ZIP 内文件被扫描并识别 |

### 4.3 评分与风险等级

评分模型按规划文档：

| 命中项 | 分数 |
|---|---:|
| SHA-256 完全命中 | 100 |
| SimHash 相似命中 | 70 |
| 高危正则命中 | 30 |
| 普通正则命中 | 15 |
| 关键词达到 `min_hits` | 30 |
| 组合规则命中 | 50 |

风险等级：

| 分数 | 置信度 / 风险 |
|---:|---|
| `>= 80` | `sensitive` / `high` |
| `50 - 79` | `suspected` / `medium` |
| `30 - 49` | `low_confidence` / `low` |
| `< 30` | `clean` / `info` |

### 4.4 SQLite 本地标签库

断言：

- `local_file_tags` 表有扫描记录；
- 至少一条记录 `sensitive = 1`；
- 重复扫描同一目录后，记录数不因相同 `(file_path, file_hash)` 重复增长；
- `client.py list --sensitive-only --json` 返回非空数组。

## 5. 文件类型覆盖建议

第一版完整脚本建议稳定覆盖：

| 类型 | 覆盖方式 |
|---|---|
| txt | 上传样本、扫描相同/相似文本 |
| py/config | 扫描 API Key、密码、数据库连接串 |
| zip | 扫描 ZIP 内敏感 txt |
| 目录递归 | 扫描 nested 子目录 |

增强脚本可继续覆盖：

| 类型 | 说明 |
|---|---|
| csv/json/xml/md | 文本类，适合加入扩展用例 |
| docx | 依赖 `python-docx`，当前 requirements 已包含 |
| xlsx | 依赖 `openpyxl`，当前 requirements 已包含 |
| pdf | 依赖 PDF 文本层，扫描件 OCR 属于增强能力 |
| 老版 Office / PPT / 图片 / 邮件 / RAR / 7z | 当前规划为后续增强，不建议作为核心验收失败条件 |

## 6. 负向与边界测试

建议覆盖：

| 用例 | 预期 |
|---|---|
| 普通文本文件 | 不应标记为敏感 |
| 不存在扫描路径 | 客户端失败退出 |
| 重复上传同一样本 | 服务端返回 409 或明确重复 |
| 无效银行卡长数字 | 不应作为银行卡命中加分 |
| 不支持格式 | 不应导致整个扫描任务崩溃 |

## 7. 当前脚本分层建议

### 7.1 Smoke Test

已有脚本：

```text
test/e2e_test.sh
```

适合快速检查 happy path：

```text
服务端可访问 → 上传一个 txt → 客户端 sync → 扫描同一个样本 → score >= 80
```

### 7.2 Full E2E Test

新增脚本：

```text
test/e2e_module_one_full.sh
```

覆盖：

1. 服务端连通性；
2. 样本上传；
3. 规则类型检查；
4. 指纹检查；
5. 重复上传检查；
6. 敏感文件 hash 查询；
7. 客户端规则同步；
8. SQLite 缓存检查；
9. 目录扫描；
10. SHA-256 精确命中；
11. 相似敏感文本命中；
12. 密钥配置文件命中；
13. 普通文件不误报；
14. ZIP 内文件扫描；
15. 本地标签查询；
16. 重复扫描 upsert 检查；
17. 扫描结果上报；
18. 服务端上报结果查询；
19. 不存在路径负向检查。

## 8. 运行方式

启动服务端后运行：

```bash
bash test/e2e_module_one_full.sh
```

可选环境变量：

```bash
E2E_SERVER_URL=http://127.0.0.1:8080
E2E_TOKEN=你的token
E2E_KEEP_TMP=1
CLIENT_PYTHON=client/.venv/Scripts/python.exe
```

说明：

- `E2E_SERVER_URL`：服务端地址；
- `E2E_TOKEN`：如果服务端开启 `SERVER_API_TOKEN`，这里传入 token；
- `E2E_KEEP_TMP=1`：失败时保留临时目录，便于排查；
- `CLIENT_PYTHON`：指定客户端 Python 解释器，未指定时脚本会优先使用 `client/.venv`。

## 9. 验收标准

完整脚本全部 PASS 即认为模块一自动化闭环通过：

1. 服务端 API 可访问；
2. 样本上传成功并生成规则、指纹、版本；
3. 客户端可同步规则；
4. 客户端可扫描指定目录；
5. 完全相同样本能 SHA-256 精确命中；
6. 相似敏感内容能被识别；
7. 密钥配置文件能被识别；
8. 普通文件不误报；
9. SQLite 本地标签库写入正常；
10. 扫描结果可上报服务端；
11. 基础负向场景行为符合预期。
