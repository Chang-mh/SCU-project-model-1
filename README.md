# SCU-project-model-1
敏感文件识别 Agent
服务端：用户上传的样本敏感文件，构建敏感文件库。
客户端：同步敏感文件库，识别指定目录下的敏感文件。
作用：识别敏感文件

## 模块一方案规划书

详见 [MODULE_ONE_PLAN.md](./MODULE_ONE_PLAN.md)。

## 模块一 MVP 目录

```text
server/   Go + Hertz 服务端：样本上传、规则生成、规则同步、指纹查询
client/   Python 客户端：规则同步、目录扫描、SQLite 本地标签库
samples/  演示样本目录
```

## 服务端启动

服务端默认使用 MySQL，启动前请创建数据库并配置 `MYSQL_DSN`：

```bash
CREATE DATABASE sensitive_agent DEFAULT CHARACTER SET utf8mb4;
```

Windows Git Bash 示例：

```bash
cd server
export MYSQL_DSN="root:password@tcp(127.0.0.1:3306)/sensitive_agent?charset=utf8mb4&parseTime=True&loc=Local"
go run .
```

默认监听 `:8080`，可通过 `SERVER_ADDR` 修改。

### 上传样本

```bash
curl -F "file=@../samples/customer.txt" \
  -F "sensitive_type=客户资料" \
  -F "risk_level=high" \
  -F "description=客户报价和联系人信息" \
  http://127.0.0.1:8080/api/server/samples
```

### 同步规则接口

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

同步规则：

```bash
python client.py sync --server http://127.0.0.1:8080
```

扫描目录：

```bash
python client.py scan --path "D:/test_docs" --server http://127.0.0.1:8080
```

查看本地敏感文件标签：

```bash
python client.py list --sensitive-only
```

扫描结果保存在客户端本地 `sensitive_tags.db`，不会修改被扫描文件。
