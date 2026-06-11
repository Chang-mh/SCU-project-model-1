package core

import (
	"strings"
	"testing"
)

func TestBuildSemanticPromptUsesNamedPlaceholders(t *testing.T) {
	prompt := buildSemanticPrompt("客户%s资料", "high", `printf("%s", secret)`)

	if strings.Contains(prompt, "{{SENSITIVE_TYPE}}") || strings.Contains(prompt, "{{RISK_LEVEL}}") || strings.Contains(prompt, "{{DOCUMENT_TEXT}}") {
		t.Fatalf("buildSemanticPrompt() left named placeholders: %q", prompt)
	}
	if !strings.Contains(prompt, "客户%s资料") {
		t.Fatalf("buildSemanticPrompt() lost sensitive type literal %%s: %q", prompt)
	}
	if !strings.Contains(prompt, `printf("%s", secret)`) {
		t.Fatalf("buildSemanticPrompt() lost document literal %%s: %q", prompt)
	}
}
