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
| `ARK_API_KEY` | 方舟 API Key，ChatModel/Embedding 共用 | 无 |
| `ARK_BASE_URL` | 方舟 OpenAI-compatible API 端点 | `https://ark.cn-beijing.volces.com/api/v3` |
| `ARK_CHAT_MODEL` | ChatModel 接入点/模型 ID | 无 |
| `ARK_EMBEDDING_MODEL` | Embedding 接入点/模型 ID；未配置时不生成向量，不影响上传 | 无 |
| `ARK_ENDPOINT_ID` | 旧版 ChatModel 配置兼容项，未设置 `ARK_CHAT_MODEL` 时使用 | 无 |
| `PADDLEOCR_API_URL` | PaddleOCR PDF 解析 HTTP 接口；占位为 `xxx` 时自动降级 | `xxx` |
| `PADDLEOCR_API_KEY` | PaddleOCR API Key；占位为 `xxx` 时不发送 Authorization | `xxx` |

**接入点 ID 是什么?** 在方舟控制台部署模型后, 系统会生成一个接入点/模型 ID。调用 API 时将它作为 `model` 参数传入。Chat 与 Embedding 请分别填入 `ARK_CHAT_MODEL` 和 `ARK_EMBEDDING_MODEL`。

未配置方舟时, 语义识别自动降级为关键词规则推理, 不影响基本功能。未配置 PaddleOCR 时，PDF 会降级为启发式文本提取。

### API 接口

上传样本:

```bash
curl -F "file=@../samples/customer.txt" \
  -F "sensitive_type=客户资料" \
  -F "risk_level=high" \
  -F "description=客户报价和联系人信息" \
  http://127.0.0.1:8080/api/server/samples
```

同步规则:

```bash
curl "http://127.0.0.1:8080/api/client/rules?version=0"
```

## 客户端使用

```bash
cd client
python -m venv .venv
.venv/Scripts/activate
pip install -r requirements.txt
```

同步规则:

```bash
python client.py sync --server http://127.0.0.1:8080
```

扫描目录:

```bash
python client.py scan --path "D:/test_docs" --server http://127.0.0.1:8080
```

查看本地敏感文件标签:

```bash
python client.py list --sensitive-only
```

扫描结果保存在客户端本地 `sensitive_tags.db`, 不会修改被扫描文件.
