package core

import (
	"strings"
	"testing"
)

func TestParseLLMResponseUsesArkChatModelName(t *testing.T) {
	t.Setenv(EnvArkChatModel, "doubao-test-model")
	t.Setenv(EnvArkEndpointID, "")

	result, err := parseLLMResponse(`{"semantic_labels":["客户资料"],"sensitive_type":"客户资料","risk_level":"high","explanation":"命中客户信息"}`, "", "")
	if err != nil {
		t.Fatalf("parseLLMResponse() error = %v", err)
	}

	if result.ModelName != "doubao-test-model" {
		t.Fatalf("parseLLMResponse().ModelName = %q, want ARK_CHAT_MODEL", result.ModelName)
	}
}

func TestParseLLMResponseFallsBackToLegacyEndpointName(t *testing.T) {
	t.Setenv(EnvArkChatModel, "")
	t.Setenv(EnvArkEndpointID, "legacy-endpoint")

	result, err := parseLLMResponse(`{"semantic_labels":["客户资料"],"sensitive_type":"客户资料","risk_level":"high","explanation":"命中客户信息"}`, "", "")
	if err != nil {
		t.Fatalf("parseLLMResponse() error = %v", err)
	}

	if result.ModelName != "legacy-endpoint" {
		t.Fatalf("parseLLMResponse().ModelName = %q, want ARK_ENDPOINT_ID", result.ModelName)
	}
}

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

func TestSemanticLabelHintsDriveRuleFallback(t *testing.T) {
	result := analyzeWithRules("内部培训资料包含未公开课件", "", "")

	if !containsString(result.SemanticLabels, "内部培训资料") {
		t.Fatalf("analyzeWithRules().SemanticLabels = %#v, want 内部培训资料", result.SemanticLabels)
	}
}
