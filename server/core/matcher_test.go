package core

import "testing"

func TestMatchBuiltinRegex(t *testing.T) {
	matches := MatchBuiltinRegex("手机号 13800138000 邮箱 test@example.com api_key = abcdefghijklmnop")

	for _, name := range []string{"mobile_phone", "email", "api_key"} {
		if !hasContentMatch(matches, name) {
			t.Fatalf("MatchBuiltinRegex() missing %q in %#v", name, matches)
		}
	}
}

func hasContentMatch(matches []ContentMatch, ruleName string) bool {
	for _, match := range matches {
		if match.RuleName == ruleName {
			return true
		}
	}
	return false
}
