package core

import (
	"regexp"
	"testing"
)

func TestGenerateRulesDetectsBuiltinRegexRules(t *testing.T) {
	text := "联系人手机号13800138000，邮箱 test@example.com，api_key = abcdefghijklmnop"

	rules := GenerateRules(text, "客户资料", "medium", "")

	for _, name := range []string{"mobile_phone", "email", "api_key"} {
		if !hasRule(rules, "regex", name) {
			t.Fatalf("GenerateRules() missing regex rule %q in %#v", name, rules)
		}
	}
}

func TestBuiltinRegexRulesArePythonCompatible(t *testing.T) {
	for _, rule := range BuiltinRegexRules {
		if _, err := regexp.Compile(rule.Pattern); err != nil {
			t.Fatalf("builtin regex %q is invalid in Go: %v", rule.Name, err)
		}
		if containsAnyValue([]string{rule.Pattern}, `\p{Han}`) {
			t.Fatalf("builtin regex %q uses \\p{Han}, which Python re cannot compile", rule.Name)
		}
	}
}

func TestGenerateRulesDetectsChinesePlateAndAddress(t *testing.T) {
	text := "车辆川A12345停放在四川省成都市高新区天府大道88号，联系人李四"

	rules := GenerateRules(text, "车辆地址资料", "medium", "")

	for _, name := range []string{"license_plate", "address"} {
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

func TestGenerateRulesDetectsTemplateCombinedRules(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{
			name: "finance",
			text: "财务预算资料：本季度成本120万元，预计利润率20%，财报未公开",
			want: "财务预算组合识别",
		},
		{
			name: "salary",
			text: "员工薪资明细：工资18000元，奖金5000元，绩效等级A",
			want: "薪资绩效组合识别",
		},
		{
			name: "source code",
			text: "源代码包含内部接口配置，api_key = abcdefghijklmnop，数据库密码需保密",
			want: "源码泄露组合识别",
		},
		{
			name: "contract",
			text: "保密合同，合同编号 HT-2026-ABC001，合同金额80万元，不得披露",
			want: "合同保密组合识别",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rules := GenerateRules(tt.text, "业务资料", "medium", "")
			if !hasRule(rules, "combined", tt.want) {
				t.Fatalf("GenerateRules() missing combined rule %q in %#v", tt.want, rules)
			}
		})
	}
}

func TestGenerateRulesKeywordExtractionUsesDomainBoosts(t *testing.T) {
	rules := GenerateRules("这份文件包含客户报价资料，客户名称和联系人需要保密", "客户资料", "medium", "客户报价单")

	var keywords []string
	for _, rule := range rules {
		if rule.RuleType == "keyword" {
			keywords, _ = rule.Content["keywords"].([]string)
			break
		}
	}
	if len(keywords) == 0 {
		t.Fatalf("GenerateRules() missing keyword rule in %#v", rules)
	}
	for _, want := range []string{"客户名称", "联系人", "报价"} {
		if !containsAnyValue(keywords, want) {
			t.Fatalf("keywords = %#v, want %q", keywords, want)
		}
	}
	if containsAnyValue(keywords, "文件") || containsAnyValue(keywords, "资料") {
		t.Fatalf("keywords = %#v, should filter common stop words", keywords)
	}
}

func TestGenerateRulesKeywordExtractionUsesJiebaBusinessTerms(t *testing.T) {
	rules := GenerateRules("甲方需缴纳履约保证金，逾期承担违约责任，付款节点按验收条款执行", "合同资料", "medium", "")

	var keywords []string
	for _, rule := range rules {
		if rule.RuleType == "keyword" {
			keywords, _ = rule.Content["keywords"].([]string)
			break
		}
	}
	if len(keywords) == 0 {
		t.Fatalf("GenerateRules() missing keyword rule in %#v", rules)
	}
	if !containsAnyValue(keywords, "履约保证金") && !containsAnyValue(keywords, "保证金") {
		t.Fatalf("keywords = %#v, want meaningful jieba business term", keywords)
	}
	for _, keyword := range keywords {
		if len([]rune(keyword)) == 1 {
			t.Fatalf("keywords = %#v, should not contain single-character noise", keywords)
		}
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

func containsAnyValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
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
