package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestGenerateEmbeddingUsesOllamaEmbedAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Fatalf("request path = %q, want /api/embed", r.URL.Path)
		}
		var req struct {
			Model string `json:"model"`
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "bge-m3" {
			t.Fatalf("request model = %q, want bge-m3", req.Model)
		}
		if req.Input != "客户报价资料" {
			t.Fatalf("request input = %q, want 客户报价资料", req.Input)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"bge-m3","embeddings":[[0.1,-0.2,0.3]]}`))
	}))
	defer server.Close()

	t.Setenv(EnvEmbeddingProvider, "ollama")
	t.Setenv(EnvOllamaBaseURL, server.URL)
	t.Setenv(EnvOllamaEmbedModel, "bge-m3")
	t.Setenv(EnvArkAPIKey, "")
	t.Setenv(EnvArkEmbeddingModel, "")

	vector, modelName, err := GenerateEmbedding("客户报价资料")
	if err != nil {
		t.Fatalf("GenerateEmbedding() error = %v", err)
	}
	if modelName != "bge-m3" {
		t.Fatalf("GenerateEmbedding() modelName = %q, want bge-m3", modelName)
	}
	want := []float64{0.1, -0.2, 0.3}
	if len(vector) != len(want) {
		t.Fatalf("GenerateEmbedding() vector len = %d, want %d", len(vector), len(want))
	}
	for i := range want {
		if vector[i] != want[i] {
			t.Fatalf("GenerateEmbedding() vector[%d] = %v, want %v", i, vector[i], want[i])
		}
	}
}

func TestGenerateEmbeddingAutoSelectsOllamaWhenModelConfigured(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embeddings":[[1,2]]}`))
	}))
	defer server.Close()

	t.Setenv(EnvEmbeddingProvider, "")
	t.Setenv(EnvOllamaBaseURL, server.URL)
	t.Setenv(EnvOllamaEmbedModel, "bge-m3")
	t.Setenv(EnvArkAPIKey, "")
	t.Setenv(EnvArkEmbeddingModel, "")

	vector, modelName, err := GenerateEmbedding("测试文本")
	if err != nil {
		t.Fatalf("GenerateEmbedding() error = %v", err)
	}
	if modelName != "bge-m3" {
		t.Fatalf("GenerateEmbedding() modelName = %q, want bge-m3", modelName)
	}
	if len(vector) != 2 || vector[0] != 1 || vector[1] != 2 {
		t.Fatalf("GenerateEmbedding() vector = %#v, want [1 2]", vector)
	}
}

func TestCosineSimilarity(t *testing.T) {
	score, ok := CosineSimilarity([]float64{1, 0}, []float64{1, 0})
	if !ok || score != 1 {
		t.Fatalf("CosineSimilarity() = %v, %v, want 1, true", score, ok)
	}

	if _, ok := CosineSimilarity([]float64{1, 0}, []float64{1}); ok {
		t.Fatal("CosineSimilarity() ok = true for mismatched vector dimensions")
	}
}
