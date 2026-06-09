# 测试样本目录

可以把用于演示的敏感样本放在这里，例如：客户报价单、薪资明细、包含手机号/邮箱/API Key 的文本文件。

示例上传：

```bash
curl -F "file=@samples/customer.txt" \
  -F "sensitive_type=客户资料" \
  -F "risk_level=high" \
  -F "description=客户报价和联系人信息" \
  http://127.0.0.1:8080/api/server/samples
```
