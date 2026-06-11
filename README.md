# SCU-project-model-1
敏感文件识别 Agent
服务端: 用户上传的样本敏感文件, 构建敏感文件库.
客户端: 同步敏感文件库, 识别指定目录下的敏感文件.
作用: 识别敏感文件

## 模块一方案规划书

详见 [MODULE_ONE_PLAN.md](./MODULE_ONE_PLAN.md).

## 模块一 MVP 目录

```text
server/   Go + Hertz 服务端: 样本上传, 规则生成, 规则同步, 指纹查询
client/   Python 客户端: 规则同步, 目录扫描, SQLite 本地标签库
samples/  演示样本目录
```

## 服务端启动

### 1. 创建数据库

```sql
CREATE DATABASE sensitive_agent DEFAULT CHARACTER SET utf8mb4;
```

### 2. 配置 .env

复制项目根目录的 `.env.example` 为 `.env`, 填入真实密钥和模型接入点:

```bash
# 火山引擎方舟 / OpenAI-compatible
ARK_API_KEY=你的真实APIKey
ARK_BASE_URL=https://ark.cn-beijing.volces.com/api/v3
ARK_CHAT_MODEL=ep-你的ChatModel接入点ID
ARK_EMBEDDING_MODEL=ep-你的Embedding接入点ID

# PaddleOCR PDF 解析；未配置时 PDF 会降级为启发式文本提取
PADDLEOCR_API_URL=你的PaddleOCR HTTP接口地址
PADDLEOCR_API_KEY=你的PaddleOCR APIKey
```

> `.env` 已在 `.gitignore` 中, 不会提交到仓库.
> 服务启动时会自动从当前目录向上查找并加载 `.env` 文件.
> `ARK_ENDPOINT_ID` 仍作为旧版 ChatModel 配置兼容项保留，推荐新配置使用 `ARK_CHAT_MODEL`.

### 3. 启动服务

```bash
cd server
go run .
```

默认监听 `:8080`, 可通过 `SERVER_ADDR` 修改.

### 火山引擎方舟配置说明

| 环境变量 | 说明 | 默认值 |
|---|---|---|
| `SERVER_API_TOKEN` | 可选 API Token；设置为非空且非 `change-me` 后所有 API 需携带 `Authorization: Bearer <token>` | `change-me` |
| `ARK_API_KEY` | 方舟 API Key，ChatModel/Embedding 共用 | 无 |
| `ARK_BASE_URL` | 方舟 OpenAI-compatible API 端点 | `https://ark.cn-beijing.volces.com/api/v3` |
| `ARK_CHAT_MODEL` | ChatModel 接入点/模型 ID | 无 |
| `ARK_EMBEDDING_MODEL` | Embedding 接入点/模型 ID；未配置时不生成向量，不影响上传 | 无 |
| `ARK_ENDPOINT_ID` | 旧版 ChatModel 配置兼容项，未设置 `ARK_CHAT_MODEL` 时使用 | 无 |
| `GOJIEBA_MODE` | 关键词抽取模式；Windows 默认留空走安全降级，需 C/C++ 编译工具链后可设为 `force` 启用 gojieba | 留空 |
| `PADDLEOCR_API_URL` | PaddleOCR PDF 解析 HTTP 接口；占位为 `xxx` 时自动降级 | `xxx` |
| `PADDLEOCR_API_KEY` | PaddleOCR API Key；占位为 `xxx` 时不发送 Authorization | `xxx` |
| `SIMHASH_THRESHOLD` | 客户端相似文件匹配的 SimHash 汉明距离阈值，会随规则同步下发 | `3` |
| `MAX_REQUEST_BODY_SIZE_MB` | Hertz 全局请求体大小上限；默认兼容 200MB 批量上传 | `220` |

**gojieba / CGO 说明：** 服务端关键词抽取已接入 `gojieba`。在 Windows 上默认不会加载 gojieba，而是使用简单分词 + 业务词库加权的安全降级方案，避免 CGO/非 ASCII 路径导致进程崩溃；如课程演示需要展示 gojieba TF-IDF 抽取效果，请先安装可用的 C/C++ 编译工具链，再在 `.env` 中设置 `GOJIEBA_MODE=force` 后启动服务。Linux/macOS 默认会尝试启用 gojieba，也可设置 `GOJIEBA_MODE=off` 强制降级。

**接入点 ID 是什么?** 在方舟控制台部署模型后, 系统会生成一个接入点/模型 ID。调用 API 时将它作为 `model` 参数传入。Chat 与 Embedding 请分别填入 `ARK_CHAT_MODEL` 和 `ARK_EMBEDDING_MODEL`。

未配置方舟时, 语义识别自动降级为关键词规则推理, 不影响基本功能。PDF 会优先使用 Go 原生文本解析；扫描件或原生解析失败时再尝试 PaddleOCR，未配置 PaddleOCR 时会降级为启发式文本提取。

### API 接口

上传样本:

```bash
curl -H "Authorization: Bearer $SERVER_API_TOKEN" \
  -F "file=@../samples/customer.txt" \
  -F "sensitive_type=客户资料" \
  -F "risk_level=high" \
  -F "description=客户报价和联系人信息" \
  http://127.0.0.1:8080/api/server/samples
```

同步规则:

```bash
curl -H "Authorization: Bearer $SERVER_API_TOKEN" "http://127.0.0.1:8080/api/client/rules?version=0"
```

上报客户端扫描结果:

```bash
curl -X POST "http://127.0.0.1:8080/api/client/scan-results" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $SERVER_API_TOKEN" \
  -d '{"host_id":"host_001","scan_path":"D:/test_docs","scanned_at":"2026-06-11T10:00:00","results":[]}'
```

重复上传相同 SHA-256 的样本时，服务端会返回 `409 Conflict` 并包含已有样本信息。

## 客户端使用

```bash
cd client
python -m venv .venv
.venv/Scripts/activate
pip install -r requirements.txt
```

同步规则:

```bash
python client.py sync --server http://127.0.0.1:8080 --token $SERVER_API_TOKEN
```

扫描目录:

```bash
python client.py scan --path "D:/test_docs" --server http://127.0.0.1:8080 --token $SERVER_API_TOKEN
```

扫描并上报结果:

```bash
python client.py scan --path "D:/test_docs" --server http://127.0.0.1:8080 --report --token $SERVER_API_TOKEN
```

查看本地敏感文件标签:

```bash
python client.py list --sensitive-only
python client.py list --sensitive-only --json
```

运行客户端测试:

```bash
.venv/Scripts/activate
python -m unittest discover -p "test_*.py" -v
```

扫描结果保存在客户端本地 `sensitive_tags.db`, 不会修改被扫描文件. 客户端会递归扫描 `.zip` 压缩包内的普通文件，并通过 `SCANNER_MAX_ZIP_DEPTH`、`SCANNER_MAX_ZIP_ENTRIES`、`SCANNER_MAX_ZIP_TOTAL_SIZE` 限制递归层级、条目数和解压总量，同时跳过存在 Zip Slip 风险的条目。
