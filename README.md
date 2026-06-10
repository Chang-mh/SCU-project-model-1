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

复制项目根目录的 `.env` 文件, 填入真实密钥:

```bash
# 火山引擎方舟
ARK_API_KEY=你的真实APIKey
ARK_ENDPOINT_ID=ep-你的真实EndpointID
```

> `.env` 已在 `.gitignore` 中, 不会提交到仓库.
> 服务启动时会自动从当前目录向上查找并加载 `.env` 文件.

### 3. 启动服务

```bash
cd server
go run .
```

默认监听 `:8080`, 可通过 `SERVER_ADDR` 修改.

### 火山引擎方舟配置说明

| 环境变量 | 说明 | 默认值 |
|---|---|---|
| `ARK_API_KEY` | 方舟 API Key (**必填**) | 无 |
| `ARK_BASE_URL` | 方舟 API 端点 | `https://ark.cn-beijing.volces.com/api/v3` |
| `ARK_ENDPOINT_ID` | 推理接入点 ID (**必填**) | 无 |

**Endpoint ID 是什么?** 在方舟控制台部署模型(如 Doubao-pro)后, 系统会生成一个推理接入点, 其唯一标识就是 Endpoint ID(格式 `ep-202406xxxxx-xxxxx`). 调用 API 时将它作为 `model` 参数传入.

未配置方舟时, 语义识别自动降级为关键词规则推理, 不影响基本功能.

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
