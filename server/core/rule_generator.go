package core

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

type RuleData struct {
	RuleName      string         `json:"rule_name"`
	RuleType      string         `json:"rule_type"`
	SensitiveType string         `json:"sensitive_type"`
	RiskLevel     string         `json:"risk_level"`
	Content       map[string]any `json:"content"`
}

type RegexRule struct {
	Name      string `json:"name"`
	Pattern   string `json:"pattern"`
	RiskLevel string `json:"risk_level"`
}

var BuiltinRegexRules = []RegexRule{
	{Name: "id_card", Pattern: `\b\d{17}[\dXx]\b`, RiskLevel: "high"},
	{Name: "mobile_phone", Pattern: `\b1[3-9]\d{9}\b`, RiskLevel: "medium"},
	{Name: "bank_card", Pattern: `\b\d{16,19}\b`, RiskLevel: "high"},
	{Name: "email", Pattern: `[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`, RiskLevel: "medium"},
	{Name: "license_plate", Pattern: `[\p{Han}][A-Z][A-Z0-9]{5,6}`, RiskLevel: "medium"},
	{Name: "passport", Pattern: `[EGPSeps][0-9]{7,8}`, RiskLevel: "medium"},
	{Name: "social_security", Pattern: `(?i)(社保|社会保障|social security).{0,12}\b\d{9,18}\b`, RiskLevel: "high"},
	{Name: "tax_number", Pattern: `(?i)(税号|纳税人识别号|tax).{0,12}[A-Z0-9]{15,20}`, RiskLevel: "high"},
	{Name: "credit_code", Pattern: `[0-9A-HJ-NPQRTUWXY]{18}`, RiskLevel: "high"},
	{Name: "contract_number", Pattern: `(?i)(合同编号|合同号|contract[ _-]?(no|id|number)).{0,12}[A-Za-z0-9\-_]{6,}`, RiskLevel: "medium"},
	{Name: "address", Pattern: `[\p{Han}]{2,}(省|市|区|县|镇|乡|街道|路|街|号|栋|单元|室)`, RiskLevel: "medium"},
	{Name: "private_ip", Pattern: `\b(10\.\d{1,3}|172\.(1[6-9]|2\d|3[0-1])|192\.168)\.\d{1,3}\.\d{1,3}\b`, RiskLevel: "medium"},
	{Name: "domain", Pattern: `\b([A-Za-z0-9\-]+\.)+[A-Za-z]{2,}\b`, RiskLevel: "low"},
	{Name: "api_key", Pattern: `(?i)(api[_-]?key|access[_-]?token|secret)[\s:=\"]+[A-Za-z0-9_\-.]{16,}`, RiskLevel: "high"},
	{Name: "access_token", Pattern: `(?i)(access[_-]?token|bearer)[\s:=\"]+[A-Za-z0-9._\-]{20,}`, RiskLevel: "high"},
	{Name: "private_key", Pattern: `-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----`, RiskLevel: "critical"},
	{Name: "password", Pattern: `(?i)(password|passwd|pwd)[\s:=\"]+[^\s\"]{6,}`, RiskLevel: "high"},
	{Name: "db_connection", Pattern: `(?i)(jdbc:mysql|postgresql://|mongodb://|redis://)`, RiskLevel: "high"},
	{Name: "money_wan", Pattern: `\d+(\.\d+)?万元`, RiskLevel: "medium"},
}

var businessKeywords = []string{
	"客户名称", "客户名单", "联系人", "联系方式", "报价", "报价单", "合同", "合同金额", "未公开", "保密",
	"财务", "预算", "薪资", "工资", "奖金", "战略规划", "商业计划", "招投标", "投标", "源代码",
	"漏洞", "运维账号", "数据库密码", "内部接口", "系统架构", "研发设计", "财报", "成本", "利润",
}

func GenerateRules(text, sensitiveType, riskLevel, description string) []RuleData {
	if riskLevel == "" {
		riskLevel = "medium"
	}
	if sensitiveType == "" {
		sensitiveType = "未分类敏感文件"
	}

	var rules []RuleData
	for _, item := range BuiltinRegexRules {
		re, err := regexp.Compile(item.Pattern)
		if err != nil || !re.MatchString(text) {
			continue
		}
		rules = append(rules, RuleData{
			RuleName:      item.Name,
			RuleType:      "regex",
			SensitiveType: sensitiveType,
			RiskLevel:     maxRisk(riskLevel, item.RiskLevel),
			Content: map[string]any{
				"name":    item.Name,
				"pattern": item.Pattern,
			},
		})
	}

	keywords := extractKeywords(text, sensitiveType, description)
	if len(keywords) > 0 {
		minHits := 2
		if len(keywords) == 1 {
			minHits = 1
		}
		rules = append(rules, RuleData{
			RuleName:      sensitiveType + "关键词识别",
			RuleType:      "keyword",
			SensitiveType: sensitiveType,
			RiskLevel:     riskLevel,
			Content: map[string]any{
				"keywords":   keywords,
				"match_mode": "any",
				"min_hits":   minHits,
			},
		})
	}

	if containsAny(text, []string{"客户", "报价", "合同金额", "联系人"}) || containsAny(description, []string{"客户", "报价"}) {
		rules = append(rules, RuleData{
			RuleName:      "客户报价组合识别",
			RuleType:      "combined",
			SensitiveType: sensitiveType,
			RiskLevel:     maxRisk(riskLevel, "high"),
			Content: map[string]any{
				"logic": "AND",
				"conditions": []map[string]any{
					{"type": "keyword", "value": []string{"客户名称", "报价", "合同金额", "联系人"}, "min_hits": 2},
					{"type": "regex", "value": `\d+(\.\d+)?万元`},
				},
			},
		})
	}

	return rules
}

func RuleContentJSON(content map[string]any) string {
	data, _ := json.Marshal(content)
	return string(data)
}

func extractKeywords(text, sensitiveType, description string) []string {
	seen := make(map[string]int)
	combined := text + " " + sensitiveType + " " + description
	for _, keyword := range businessKeywords {
		if strings.Contains(combined, keyword) {
			seen[keyword] += 10
		}
	}

	for _, token := range tokenizeWords(combined) {
		if len([]rune(token)) < 2 || isStopWord(token) {
			continue
		}
		seen[token]++
	}

	type pair struct {
		word  string
		count int
	}
	var pairs []pair
	for word, count := range seen {
		pairs = append(pairs, pair{word: word, count: count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count == pairs[j].count {
			return pairs[i].word < pairs[j].word
		}
		return pairs[i].count > pairs[j].count
	})

	limit := 12
	if len(pairs) < limit {
		limit = len(pairs)
	}
	keywords := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		keywords = append(keywords, pairs[i].word)
	}
	return keywords
}

func tokenizeWords(text string) []string {
	var tokens []string
	var current []rune
	flush := func() {
		if len(current) > 0 {
			tokens = append(tokens, string(current))
			current = nil
		}
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r >= 0x4e00 && r <= 0x9fff {
			current = append(current, r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func isStopWord(word string) bool {
	stopWords := map[string]bool{"文件": true, "文档": true, "内容": true, "信息": true, "数据": true, "the": true, "and": true, "this": true}
	return stopWords[strings.ToLower(word)]
}

func containsAny(text string, values []string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}

func maxRisk(a, b string) string {
	order := map[string]int{"info": 0, "low": 1, "medium": 2, "high": 3, "critical": 4}
	if order[b] > order[a] {
		return b
	}
	return a
}
