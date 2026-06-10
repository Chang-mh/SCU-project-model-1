package core

import "testing"

func TestGenerateRulesDetectsBuiltinRegexRules(t *testing.T) {
	text := "联系人手机号13800138000，邮箱 test@example.com，api_key = abcdefghijklmnop"

	rules := GenerateRules(text, "客户资料", "medium", "")

	for _, name := range []string{"mobile_phone", "email", "api_key"} {
		if !hasRule(rules, "regex", name) {
			t.Fatalf("GenerateRules() missing regex rule %q in %#v", name, rules)
		}
	}
}

func TestGenerateRulesDetectsCustomerQuoteCombinedRule(t *testing.T) {
	text := "客户名称：四川示例科技有限公司\n联系人：张三\n合同金额：50万元\n报价：48万元"

	rules := GenerateRules(text, "客户资料", "medium", "客户报价资料")

	if !hasRule(rules, "combined", "客户报价组合识别") {
		t.Fatalf("GenerateRules() missing customer quote combined rule in %#v", rules)
	}
}

func TestAnalyzeSemanticFallsBackToRules(t *testing.T) {
	t.Setenv(EnvArkAPIKey, "")
	t.Setenv(EnvArkEndpointID, "")

	result := AnalyzeSemantic("客户名称：四川示例科技有限公司，联系人：张三，报价：50万元", "", "")

	if result.ModelName != "rule-fallback" {
		t.Fatalf("AnalyzeSemantic().ModelName = %q, want rule-fallback", result.ModelName)
	}
	if result.RiskLevel != "high" {
		t.Fatalf("AnalyzeSemantic().RiskLevel = %q, want high", result.RiskLevel)
	}
	if !containsString(result.SemanticLabels, "客户名单") {
		t.Fatalf("AnalyzeSemantic().SemanticLabels = %#v, want 客户名单", result.SemanticLabels)
	}
	if !containsString(result.SemanticLabels, "报价信息") {
		t.Fatalf("AnalyzeSemantic().SemanticLabels = %#v, want 报价信息", result.SemanticLabels)
	}
}

func hasRule(rules []RuleData, ruleType, ruleName string) bool {
	for _, rule := range rules {
		if rule.RuleType == ruleType && rule.RuleName == ruleName {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
