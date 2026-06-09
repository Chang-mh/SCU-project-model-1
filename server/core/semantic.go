package core

import "strings"

type SemanticResult struct {
	SemanticLabels []string `json:"semantic_labels"`
	SensitiveType  string   `json:"sensitive_type"`
	RiskLevel      string   `json:"risk_level"`
	EmbeddingID    string   `json:"embedding_id"`
	Explanation    string   `json:"explanation"`
}

func AnalyzeSemantic(text, sensitiveType, riskLevel string) SemanticResult {
	labels := inferLabels(text + " " + sensitiveType)
	if sensitiveType == "" && len(labels) > 0 {
		sensitiveType = strings.Join(labels, "/")
	}
	if sensitiveType == "" {
		sensitiveType = "未分类敏感文件"
	}
	if riskLevel == "" {
		riskLevel = inferRisk(labels)
	}
	if len(labels) == 0 {
		labels = []string{sensitiveType}
	}
	return SemanticResult{
		SemanticLabels: labels,
		SensitiveType:  sensitiveType,
		RiskLevel:      riskLevel,
		EmbeddingID:    "",
		Explanation:    "文档命中敏感样本规则，包含" + strings.Join(labels, "、") + "等敏感语义或业务关键词。",
	}
}

func inferLabels(text string) []string {
	candidates := map[string][]string{
		"客户名单":   {"客户", "联系人", "客户名称", "名单"},
		"报价信息":   {"报价", "合同金额", "万元", "价格"},
		"财务预算":   {"财务", "预算", "成本", "利润", "财报"},
		"薪资明细":   {"薪资", "工资", "奖金", "绩效"},
		"保密协议":   {"保密", "协议", "不得披露"},
		"研发设计文档": {"研发", "设计", "架构", "技术方案"},
		"源代码说明":  {"源代码", "接口", "函数", "数据库连接"},
		"运维账号":   {"账号", "密码", "token", "secret", "运维"},
		"安全漏洞信息": {"漏洞", "CVE", "修复", "攻击"},
		"战略规划":   {"战略", "规划", "商业计划", "未公开"},
	}
	var labels []string
	for label, words := range candidates {
		for _, word := range words {
			if strings.Contains(strings.ToLower(text), strings.ToLower(word)) {
				labels = append(labels, label)
				break
			}
		}
	}
	return labels
}

func inferRisk(labels []string) string {
	for _, label := range labels {
		if label == "运维账号" || label == "安全漏洞信息" || label == "客户名单" || label == "报价信息" {
			return "high"
		}
	}
	if len(labels) > 0 {
		return "medium"
	}
	return "low"
}
