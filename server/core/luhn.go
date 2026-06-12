package core

func LuhnCheck(number string) bool {
	if len(number) == 0 {
		return false
	}

	sum := 0
	double := false
	for i := len(number) - 1; i >= 0; i-- {
		ch := number[i]
		if ch < '0' || ch > '9' {
			return false
		}
		digit := int(ch - '0')
		if double {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
		double = !double
	}
	return sum%10 == 0
}

func isValidBankCard(number string) bool {
	if len(number) < 16 || len(number) > 19 {
		return false
	}
	return LuhnCheck(number)
}

func filterRegexMatches(ruleName string, matches []string) []string {
	if ruleName != "bank_card" {
		return matches
	}
	filtered := make([]string, 0, len(matches))
	for _, match := range matches {
		if isValidBankCard(match) {
			filtered = append(filtered, match)
		}
	}
	return filtered
}
