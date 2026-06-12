package core

import "testing"

func TestLuhnCheck(t *testing.T) {
	cases := []struct {
		name   string
		number string
		want   bool
	}{
		{name: "valid bank card", number: "4111111111111111", want: true},
		{name: "invalid check digit", number: "4111111111111112", want: false},
		{name: "too short", number: "1234567890", want: false},
		{name: "non digit", number: "622202100111624X", want: false},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidBankCard(tt.number); got != tt.want {
				t.Fatalf("isValidBankCard(%q) = %v, want %v", tt.number, got, tt.want)
			}
		})
	}
}

func TestBankCardRegexRequiresLuhnCheck(t *testing.T) {
	validMatches := MatchBuiltinRegex("银行卡号 4111111111111111")
	if !hasContentMatch(validMatches, "bank_card") {
		t.Fatalf("MatchBuiltinRegex() should keep Luhn-valid bank card: %#v", validMatches)
	}

	invalidMatches := MatchBuiltinRegex("普通流水号 4111111111111112")
	if hasContentMatch(invalidMatches, "bank_card") {
		t.Fatalf("MatchBuiltinRegex() should filter Luhn-invalid bank card: %#v", invalidMatches)
	}
}
