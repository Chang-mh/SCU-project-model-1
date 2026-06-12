package core

import "regexp"

type ContentMatch struct {
	RuleName  string   `json:"rule_name"`
	Pattern   string   `json:"pattern"`
	RiskLevel string   `json:"risk_level"`
	Matches   []string `json:"matches"`
}

func MatchBuiltinRegex(content string) []ContentMatch {
	results := make([]ContentMatch, 0)
	for _, rule := range BuiltinRegexRules {
		re, err := regexp.Compile(rule.Pattern)
		if err != nil {
			continue
		}
		matches := filterRegexMatches(rule.Name, re.FindAllString(content, -1))
		if len(matches) == 0 {
			continue
		}
		results = append(results, ContentMatch{
			RuleName:  rule.Name,
			Pattern:   rule.Pattern,
			RiskLevel: rule.RiskLevel,
			Matches:   matches,
		})
	}
	return results
}
