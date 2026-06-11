package core

import (
	"encoding/json"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/yanyiwu/gojieba"
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
	{Name: "license_plate", Pattern: `[一-龥][A-Z][A-Z0-9]{5,6}`, RiskLevel: "medium"},
	{Name: "passport", Pattern: `[EGPSeps][0-9]{7,8}`, RiskLevel: "medium"},
	{Name: "social_security", Pattern: `(?i)(社保|社会保障|social security).{0,12}\b\d{9,18}\b`, RiskLevel: "high"},
	{Name: "tax_number", Pattern: `(?i)(税号|纳税人识别号|tax).{0,12}[A-Z0-9]{15,20}`, RiskLevel: "high"},
	{Name: "credit_code", Pattern: `[0-9A-HJ-NPQRTUWXY]{18}`, RiskLevel: "high"},
	{Name: "contract_number", Pattern: `(?i)(合同编号|合同号|contract[ _-]?(no|id|number)).{0,12}[A-Za-z0-9\-_]{6,}`, RiskLevel: "medium"},
	{Name: "address", Pattern: `[一-龥]{2,}(省|市|区|县|镇|乡|街道|路|街|号|栋|单元|室)`, RiskLevel: "medium"},
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
	"客户名称", "客户名单", "联系人", "联系方式", "报价", "报价单", "合同", "合同编号", "合同金额", "未公开", "保密", "不得披露",
	"财务", "预算", "薪资", "工资", "奖金", "绩效", "战略规划", "商业计划", "招投标", "投标", "源代码",
	"漏洞", "运维账号", "数据库密码", "内部接口", "系统架构", "研发设计", "财报", "成本", "利润", "API Key", "access token",
	"履约保证金", "付款节点", "验收条款", "违约责任", "甲方", "乙方", "未公开财报", "内部培训资料", "研发设计文档",
}

var stopWords = map[string]bool{
	"的": true, "了": true, "在": true, "是": true, "我": true, "有": true, "和": true, "就": true,
	"不": true, "人": true, "都": true, "一": true, "一个": true, "上": true, "也": true, "很": true,
	"到": true, "说": true, "要": true, "去": true, "你": true, "会": true, "着": true, "没有": true,
	"看": true, "好": true, "自己": true, "这": true, "那": true, "他": true, "她": true, "它": true,
	"们": true, "什么": true, "为": true, "所以": true, "因为": true, "但是": true, "可以": true,
	"这个": true, "那个": true, "进行": true, "使用": true, "以及": true, "通过": true, "需要": true,
	"文件": true, "文档": true, "内容": true, "信息": true, "数据": true, "资料": true, "包含": true, "相关": true,
	"the": true, "and": true, "this": true, "that": true, "with": true, "for": true, "from": true, "have": true, "has": true,
}

var (
	jiebaOnce      sync.Once
	jiebaSegmenter *gojieba.Jieba
)

type combinedRuleTemplate struct {
	Name       string
	Keywords   []string
	MinHits    int
	Regex      string
	RiskLevel  string
	TriggerAny []string
}

var combinedRuleTemplates = []combinedRuleTemplate{
	{
		Name:       "客户报价组合识别",
		Keywords:   []string{"客户名称", "报价", "合同金额", "联系人"},
		MinHits:    2,
		Regex:      `\d+(\.\d+)?万元`,
		RiskLevel:  "high",
		TriggerAny: []string{"客户", "报价", "合同金额", "联系人"},
	},
	{
		Name:       "财务预算组合识别",
		Keywords:   []string{"财务", "预算", "成本", "利润", "财报"},
		MinHits:    2,
		Regex:      `\d+(\.\d+)?(万元|元|%)`,
		RiskLevel:  "high",
		TriggerAny: []string{"财务", "预算", "成本", "利润", "财报"},
	},
	{
		Name:       "薪资绩效组合识别",
		Keywords:   []string{"薪资", "工资", "奖金", "绩效", "员工"},
		MinHits:    2,
		Regex:      `\d+(\.\d+)?(万元|元)`,
		RiskLevel:  "high",
		TriggerAny: []string{"薪资", "工资", "奖金", "绩效"},
	},
	{
		Name:       "源码泄露组合识别",
		Keywords:   []string{"源代码", "接口", "函数", "数据库密码", "内部接口"},
		MinHits:    2,
		Regex:      `(?i)(api[_-]?key|password|passwd|secret|token)[\s:=\"]+[^\s\"]{6,}`,
		RiskLevel:  "critical",
		TriggerAny: []string{"源代码", "接口", "数据库密码", "内部接口"},
	},
	{
		Name:       "合同保密组合识别",
		Keywords:   []string{"合同", "合同编号", "合同金额", "保密", "不得披露"},
		MinHits:    2,
		Regex:      `(?i)(合同编号|合同号|contract[ _-]?(no|id|number)).{0,12}[A-Za-z0-9\-_]{6,}`,
		RiskLevel:  "high",
		TriggerAny: []string{"合同", "合同编号", "合同金额", "保密"},
	},
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

	for _, template := range combinedRuleTemplates {
		if !containsAny(text, template.TriggerAny) && !containsAny(description, template.TriggerAny) && !containsAny(sensitiveType, template.TriggerAny) {
			continue
		}
		rules = append(rules, RuleData{
			RuleName:      template.Name,
			RuleType:      "combined",
			SensitiveType: sensitiveType,
			RiskLevel:     maxRisk(riskLevel, template.RiskLevel),
			Content: map[string]any{
				"logic": "AND",
				"conditions": []map[string]any{
					{"type": "keyword", "value": template.Keywords, "min_hits": template.MinHits},
					{"type": "regex", "value": template.Regex},
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
	combined := text + " " + sensitiveType + " " + description

	scores := make(map[string]float64)
	addScore := func(word string, score float64) {
		word = strings.TrimSpace(word)
		if !isMeaningfulKeyword(word) {
			return
		}
		scores[word] += score
	}

	if canUseJieba() {
		jieba := getJieba()
		for rank, keyword := range jieba.Extract(combined, 30) {
			addScore(keyword, float64(60-rank))
		}
		for _, token := range jieba.CutForSearch(combined, true) {
			addScore(token, 1)
		}
	} else {
		for _, token := range tokenizeWords(combined) {
			addScore(token, 1)
		}
	}

	for _, keyword := range businessKeywords {
		combinedLower := strings.ToLower(combined)
		keywordLower := strings.ToLower(keyword)
		if strings.Contains(combinedLower, keywordLower) {
			addScore(keyword, 80)
		}
		if sensitiveType != "" && strings.Contains(strings.ToLower(sensitiveType), keywordLower) {
			addScore(keyword, 30)
		}
		if description != "" && strings.Contains(strings.ToLower(description), keywordLower) {
			addScore(keyword, 40)
		}
	}

	type pair struct {
		word  string
		score float64
	}
	pairs := make([]pair, 0, len(scores))
	for word, score := range scores {
		pairs = append(pairs, pair{word: word, score: score})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].score == pairs[j].score {
			return pairs[i].word < pairs[j].word
		}
		return pairs[i].score > pairs[j].score
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

func canUseJieba() bool {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("GOJIEBA_MODE")))
	if mode == "off" || mode == "false" || mode == "0" {
		return false
	}
	if mode == "force" || mode == "on" || mode == "true" || mode == "1" {
		return true
	}
	// gojieba/cppjieba may crash in some Windows CGO environments, especially when
	// the project path contains non-ASCII characters. Keep the dependency and
	// support explicit opt-in while preserving a safe default for tests/builds.
	return runtime.GOOS != "windows"
}

func getJieba() *gojieba.Jieba {
	jiebaOnce.Do(func() {
		jiebaSegmenter = gojieba.NewJieba()
		for _, keyword := range businessKeywords {
			jiebaSegmenter.AddWord(keyword)
		}
	})
	return jiebaSegmenter
}

func isMeaningfulKeyword(word string) bool {
	word = strings.TrimSpace(word)
	if word == "" || isStopWord(word) {
		return false
	}
	runes := []rune(word)
	if len(runes) < 2 || len(runes) > 32 {
		return false
	}
	allDigit := true
	for _, r := range runes {
		if !unicode.IsDigit(r) {
			allDigit = false
			break
		}
	}
	if allDigit {
		return false
	}
	return true
}

func cjkNgrams(text string, minN, maxN int) []string {
	var grams []string
	var runes []rune
	flush := func() {
		for n := minN; n <= maxN; n++ {
			if len(runes) < n {
				continue
			}
			for i := 0; i+n <= len(runes); i++ {
				grams = append(grams, string(runes[i:i+n]))
			}
		}
		runes = nil
	}
	for _, r := range text {
		if r >= 0x4e00 && r <= 0x9fff {
			runes = append(runes, r)
			continue
		}
		flush()
	}
	flush()
	return grams
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
