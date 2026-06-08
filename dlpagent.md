# 终端敏感数据外发监控系统

## 一、系统总体定位

### 1.1 核心目标

系统用于识别企业内部敏感文件，监控终端文件操作行为，发现潜在外发、泄露、异常访问等风险，并通过 AI Agent 对告警进行降噪、归因、分析和统计报表生成。

### 1.2 典型应用场景

1. 企业内部文档、合同、源代码、客户资料、财务报表等敏感文件识别。
2. 监控员工是否通过浏览器、聊天软件、邮件、网盘、压缩工具等外发敏感文件。
3. 对大量终端日志进行去重、聚合、误报过滤、威胁评估。
4. 支持安全运营人员通过自然语言查询，例如："最近一周哪些部门外发敏感文件最多？"
5. 自动生成日报、周报、专项调查报告。

## 二、整体系统架构

### 2.1 模块组成

系统可拆分为四个核心模块：

1. **敏感文件识别 Agent**

- 服务端：用户上传的样本敏感文件，构建敏感文件库。
- 客户端：同步敏感文件库，识别指定目录下的敏感文件
- 作用：识别敏感文件

2. **敏感文件外发监控客户端**

- 部署位置：员工终端。
- 作用：监控文件外发行为并上报日志。

3. **告警日志评估 Agent**

- 输入：客户端上报的文件外发日志。
- 输出：重复告警合并，白名单，历史误报检索。
- 作用：降低噪声，识别真正有价值的安全事件。

4. **日志统计分析 Agent**

- 输入：用户自然语言问题、历史告警数据、统计维度。
- 输出：SQL/查询结果、图表、分析说明、报告。
- 作用：辅助安全运营、管理层汇报和专项调查。

## 三、模块一：敏感文件识别 Agent

### 3.1 核心目标

将用户上传的敏感文件自动转换为可执行的识别规则，包括：

- 正则表达式；
- 关键词规则；
- 文本特征指纹；
- 向量化特征（可选）；

### 3.2 输入内容

用户可上传以下类型文件：

| 类型 | 示例 |
|------|------|
| 文本文件 | TXT、CSV、JSON、XML、Markdown |
| 办公文档（可选） | Word、Excel、PPT、PDF |
| 代码文件（可选） | Java、Go、Python、SQL、配置文件 |

### 3.3 敏感内容提取能力

Agent 需要识别以下敏感内容。

#### 3.3.1 固定格式敏感信息

- 身份证号；
- 手机号；
- 银行卡号；
- 邮箱地址；
- 地址；
- 车牌号；
- 护照号；
- 社保号；
- 税号；
- 统一社会信用代码；
- API Key；
- Access Token；
- 私钥；
- 密码；
- 数据库连接串；
- 内网 IP；
- 域名；
- 账号凭证。

#### 3.3.2 企业业务敏感信息

- 合同编号；
- 客户名称；
- 客户联系方式；
- 项目名称；
- 报价信息；
- 财务数据；
- 薪酬数据；
- 组织架构；
- 商业计划；
- 招投标文件；
- 源代码；
- 内部接口文档；
- 系统架构图；
- 安全漏洞信息；
- 运维账号信息。

#### 3.3.3 文档语义特征

Agent 不仅要提取固定格式内容，还要识别文档语义，例如：

- "保密协议"；
- "客户名单"；
- "财务预算"；
- "报价单"；
- "薪资明细"；
- "研发设计文档"；
- "源代码说明"；
- "内部培训资料"；
- "未公开财报"；
- "战略规划"。

### 3.4 识别规则生成

Agent 输出的识别规则可分为几类。

#### 3.4.1 正则规则

用于匹配结构化敏感信息，例如：

```regex
\d{17}[\dXx]
```

#### 3.4.2 关键词规则

例如：

```json
{
  "keywords": ["客户名单", "报价单", "未公开", "保密", "薪资", "合同金额"],
  "match_mode": "any",
  "min_hits": 2
}
```

#### 3.4.3 组合规则

例如：

```json
{
  "rule_name": "客户报价单识别",
  "conditions": [
    {
      "type": "keyword",
      "value": ["客户名称", "报价", "合同金额"]
    },
    {
      "type": "regex",
      "value": "\\d+(\\.\\d+)?万元"
    }
  ],
  "logic": "AND"
}
```

#### 3.4.4 文件指纹

对文件内容进行哈希或局部特征提取，用于识别近似文件或变形文件。

指纹类型包括：

- 完整文件 hash；
- SimHash；

#### 3.4.5 文件语义向量（可选支持）

调用大模型 embedding 构建向量数据库

### 3.5 输出结果示例

```json
{
  "sensitive_file_id": "file_001",
  "sensitive_file_name": "2025年度客户报价表.xlsx",
  "sensitive_type": "客户资料/报价信息",
  "risk_level": "high",
  "generated_rules": [
    {
      "type": "keyword",
      "value": ["客户名称", "报价", "联系人", "合同金额"]
    },
    {
      "type": "regex",
      "value": "\\d+(\\.\\d+)?万元"
    }
  ],
  "fingerprint": {
    "hash": "xxx",
    "simhash": "xxx"
  },
  "embedding": [
      0.0023064255, -0.009327292, 0.015789012, -0.004567890,
      0.008765432, -0.012345678, 0.003456789, -0.006789012,
      ... // 共1536个float32值
      0.001234567, -0.005678901
  ], // 可选支持
  "explanation": "文件中包含客户名称、联系人、报价金额等多个敏感字段。"
}
```

### 3.6 文件扫描能力（客户端）

#### 3.6.1 扫描模式

- 接收指定目录进行扫描

#### 3.6.2 文本提取能力

客户端需要支持从不同格式文件中提取文本。

| 文件类型 | 处理方式 |
|----------|----------|
| txt/csv/json/xml | 直接读取 |
| doc/docx（可选） | Office 文档解析 |
| xls/xlsx（可选） | 表格解析 |
| ppt/pptx（可选） | 幻灯片文本解析 |
| pdf（可选） | PDF 文本提取，必要时 OCR |
| 图片（可选） | OCR |
| zip/rar/7z（可选） | 解压后递归扫描 |
| eml/msg（可选） | 邮件正文与附件解析 |
| 源代码（可选） | 代码文本扫描 |
| 二进制文件（可选） | hash、元数据、特征扫描 |

#### 3.6.3 敏感文件标记

识别为敏感文件后，需要给文件打上内部标签。

标签内容可包括：

```json
{
  "file_path": "C:/Users/A/Desktop/customer.xlsx",
  "file_hash": "xxx",
  "sensitive": true,
  "sensitive_type": "客户资料",
  "risk_level": "high",
  "sensitive_file_id": "file_001",
  "first_detected_at": "2026-06-02 10:00:00",
  "last_detected_at": "2026-06-02 11:00:00"
}
```

标记方式可选：

1. 本地轻量数据库记录；
2. 文件扩展属性；
3. 隐藏 sidecar 文件；
4. 服务端统一登记；
5. 文件 hash 与路径映射。

建议优先使用：

- 本地 SQLite；
- 文件 hash 作为稳定标识；
- 路径作为辅助标识。

## 四、模块二：敏感文件外发监控客户端

### 4.1 核心目标

- 监控敏感文件访问、复制、压缩、上传、发送等行为；
- 上报告警日志。

### 4.2 客户端部署对象

| 场景 | 示例 |
|------|------|
| 员工电脑 | Windows、Linux （任选其一） |

### 4.6 文件行为监控

客户端需要监控以下行为。

#### 4.6.1 文件访问行为

- 打开；
- 读取；
- 复制；
- 打包压缩；

#### 4.6.2 外发行为

重点监控敏感文件是否被以下软件或进程读取、上传、发送：

| 类型 | 示例 |
|------|------|
| 浏览器 | Chrome |
| 聊天软件 | 微信 |
| 邮件客户端 | Outlook或者Foxmail |
| 网盘 | 百度网盘 |
| 压缩软件 | WinRAR或者7zip |

### 4.7 日志上报内容

客户端上报日志应包括：

```json
{
  "event_id": "evt_001",
  "host_id": "host_001",
  "user_id": "user_001",
  "file_path": "C:/Users/A/Desktop/customer.xlsx",
  "file_hash": "xxx",
  "sensitive": true,
  "sensitive_type": "客户资料",
  "risk_level": "high",
  "process_name": "chrome.exe",
  "process_path": "C:/Program Files/Google/Chrome/Application/chrome.exe",
  "target": "", // 外发目标，自己生成只用于测试。浏览器是域名，压缩是文件路径，邮件是邮箱地址，可为空
  "operation": "upload",
  "timestamp": 177777934,
  "sensitive_file_id": "file_001"
}
```

## 五、模块三：告警日志误报分析 Agent（客户端）

### 5.1 核心目标

客户端会产生大量日志，其中很多可能是重复、低风险、误报或正常业务行为。告警日志评估 Agent 负责：

1. 合并重复日志；
2. 白名单支持；
3. 检索历史误报；

**需要从监控客户端接收日志**

### 5.2 前置规则过滤

#### 5.2.1 重复日志合并

需求：

如果同一用户、同一主机、同一软件、短时间内产生大量相似日志，需要合并为一条聚合告警。

聚合维度：

- 用户；
- 主机；
- 进程；
- 文件类型；
- 敏感类型；
- 时间窗口；
- 操作类型。

示例：

```text
1 分钟内 chrome.exe 上传了 30 个客户资料文件
```

合并后输出：

```json
{
  "event_id": "evt_001",
  "host_id": "host_001",
  "user_id": "user_001",
  "operation": "upload"
  "sensitive_type": "客户资料",
  "process_name": "chrome.exe",
  "risk_level": "high"
  "is_merge_event": true
  "file_count": 30,
  "files": [
    {
    "file_path": "C:/Users/A/Desktop/customer.xlsx",
    "file_hash": "xxx"
    } // ....
  ],
  "time_range": "1777732343-1777234343",
  "duration": "1m",
}
```

#### 5.2.2 白名单过滤

可配置白名单：

- 软件白名单；
- 用户白名单；
- 文件路径白名单；
- 时间窗口白名单；

例如：

- 备份程序读取敏感文件可忽略；
- DLP 扫描程序自身行为不告警。

**白名单过滤后直接丢弃该日志**

### 5.3 历史误报检索

**历史误报数据自己生成**

系统需要维护误报库，用于减少重复误报。

误报库字段建议：

```json
{
  "host_id": "host_001",
  "user_id": "user_001",
  "sensitive_type": "客户资料",
  "risk_level": "high",
  "process_name": "chrome.exe",
  "process_path": "C:/Program Files/Google/Chrome/Application/chrome.exe",
  "target": "internal.company.com", // 外发目标，自己生成只用于测试。浏览器是域名，压缩是文件路径，邮件是邮箱地址，可为空
  "operation": "upload",
  "reason": "user_001企业内部 CRM 系统上传客户资料"
}
```

Agent 评估时应：

1. 检索相似历史误报；
2. 判断当前事件是否匹配误报条件；
3. 如果匹配，则降级或标记为low；
4. 不匹配输出原日志

### 5.5 Agent 评估输出

告警评估 Agent 应输出：

```json
{
  "event_id": "evt_001",
  "host_id": "host_001",
  "user_id": "user_001",
  "file_path": "C:/Users/A/Desktop/customer.xlsx",
  "file_hash": "xxx",
  "sensitive": true,
  "sensitive_type": "客户资料",
  "risk_level": "info",
  "process_name": "chrome.exe",
  "process_path": "C:/Program Files/Google/Chrome/Application/chrome.exe",
  "target": "", // 外发目标，自己生成只用于测试。浏览器是域名，压缩是文件路径，邮件是邮箱地址，可为空
  "operation": "upload",
  "timestamp": 177777934,
  "sensitive_file_id": "file_001"
  "old_risk_level": "high",
  "false_positive_reason": "" // 误报原因
}
```

### 5.8 风险等级定义

| 等级 | 说明 |
|------|------|
| critical | 明确高危外发，行为链完整，涉及高敏文件 |
| high | 高度可疑，存在多个风险因素 |
| medium | 存在异常，但证据不完整 |
| low | 低风险行为，建议记录 |
| info | 普通日志，不产生告警 |

## 六、模块四：日志统计分析 Agent（服务端）

### 6.1 核心目标

将用户自然语言转换为数据库查询、统计分析和可视化报告。

用户可以直接提问：

- "最近 7 天哪个部门敏感文件外发最多？"
- "帮我统计本月高危告警趋势。"
- "张三最近一个月访问了哪些敏感文件？"
- "生成一份本周数据泄露风险报告。"
- "对比销售部和研发部的敏感文件操作情况。"

**告警日志误报分析接收日志，存储格式自己定义。用户、部门，主机数据需要自己生成**

### 6.2 能力要求

#### 6.2.1 自然语言理解

Agent 需要理解：

- 时间范围；
- 用户；
- 部门；
- 主机；
- 文件类型；
- 敏感类型；
- 风险等级；
- 操作类型；
- 统计指标；
- 排序方式；
- 输出格式。

#### 6.2.2 自动生成查询（存储数据库没有要求）

根据用户问题生成：

- MySQL；

例如：

用户问：

```text
最近一周哪些用户上传高敏文件最多？
```

系统生成：

```sql
SELECT user_id, COUNT(*) AS upload_count
FROM alert_logs
WHERE risk_level IN ('high', 'critical')
  AND event_type = 'file_upload'
  AND timestamp >= NOW() - INTERVAL '7 days'
GROUP BY user_id
ORDER BY upload_count DESC
LIMIT 10;
```

#### 6.2.3 查询安全控制（可选）

必须避免 Agent 任意执行危险 SQL。

需要限制：

1. 只允许 SELECT；
2. 禁止 DELETE、UPDATE、DROP、INSERT；
3. 查询必须带权限校验；
4. 查询必须带数据范围限制；
5. 查询必须经过 SQL AST 校验；
6. 查询结果脱敏；
7. 限制最大返回条数；
8. 限制最大查询时间；
9. 敏感字段按权限展示。

### 6.3 报表生成能力

支持生成：

- 日报；
- 周报；
- 月报；
- 部门报告；
- 高危事件复盘报告；
- 趋势分析报告。

### 6.4 图表类型

系统应根据数据自动选择合适图表：

| 数据类型 | 图表 |
|----------|------|
| 时间趋势 | 折线图、面积图 |
| 排名统计 | 柱状图、排行榜 |
| 占比分析 | 饼图、环形图 |
| 部门对比 | 分组柱状图 |

### 6.5 多轮对话能力（可选）

支持连续查询：

用户：

```text
查一下最近 7 天高危告警。
```

Agent 返回结果后，用户继续：

```text
只看销售部。
```

Agent 应理解上下文：

```text
最近 7 天 + 高危告警 + 销售部
```

用户继续：

```text
生成一份报告。
```

Agent 应基于前两轮查询结果生成报告。

### 6.6 报告输出格式

支持：

- Markdown；
- HTML；

### 6.7 报告内容结构

一份标准报告可包含：

1. 报告标题；
2. 时间范围；
3. 数据范围；
4. 核心结论；
5. 关键指标；
6. 趋势图；
7. Top 用户；
8. Top 部门；
9. Top 外发渠道；
10. 高危事件列表；
11. 风险原因；
12. 处置建议；
13. 附录数据。

## 七、关键接口设计（建议）

### 7.1 客户端规则拉取接口

```http
GET /api/client/rules?version=10
```

响应：

```json
{
  "latest_version": 11,
  "rules": []
}
```

### 7.2 告警日志误报分析Agent客户端日志上报接口

```http
POST /api/client/events
```

请求：

```json
{
  "host_id": "host_001",
  "events": [
  {
    // 原始日志
    "event_id": "evt_001",
    "host_id": "host_001",
    "user_id": "user_001",
    "file_path": "C:/Users/A/Desktop/customer.xlsx",
    "file_hash": "xxx",
    "sensitive": true,
    "sensitive_type": "客户资料",
    "risk_level": "info",
    "process_name": "chrome.exe",
    "process_path": "C:/Program Files/Google/Chrome/Application/chrome.exe",
    "target": "", // 外发目标，自己生成只用于测试。浏览器是域名，压缩是文件路径，邮件是邮箱地址，可为空
    "operation": "upload",
    "timestamp": 177777934,
    "sensitive_file_id": "file_001"

    // 误报分析
    "old_risk_level": "high",
    "false_positive_reason": "" // 误报原因

    // 合并告警
    "is_merge_event": true
    "file_count": 30,
    "files": [
      {
      "file_path": "C:/Users/A/Desktop/customer.xlsx",
      "file_hash": "xxx"
      } // ....
    ],
    "time_range": "1777732343-1777234343",
    "duration": "1m",
  }
  ]
}
```

### 7.3 自然语言查询接口

```http
POST /api/analysis/query
```

请求：

```json
{
  "session_id": "session_001",
  "question": "最近一周哪些用户外发敏感文件最多？"
}
```

响应：

```text
报告
```
