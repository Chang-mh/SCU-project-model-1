package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

//go:embed semantic_prompt.txt
var semanticPromptTemplate string

type SemanticResult struct {
	SemanticLabels []string  `json:"semantic_labels"`
	SensitiveType  string    `json:"sensitive_type"`
	RiskLevel      string    `json:"risk_level"`
	EmbeddingID    string    `json:"embedding_id"`
	Embedding      []float64 `json:"embedding,omitempty"`
	ModelName      string    `json:"model_name"`
	Explanation    string    `json:"explanation"`
}

// 火山引擎方舟大模型配置
const (
	EnvArkAPIKey         = "ARK_API_KEY"         // 火山方舟 API Key, 替换 xxxxx 为真实 key
	EnvArkBaseURL        = "ARK_BASE_URL"        // 可选, 默认方舟 OpenAI 兼容端点
	EnvArkChatModel      = "ARK_CHAT_MODEL"      // 方舟 ChatModel 接入点/模型 ID
	EnvArkEmbeddingModel = "ARK_EMBEDDING_MODEL" // 方舟 Embedding 接入点/模型 ID
	EnvArkEndpointID     = "ARK_ENDPOINT_ID"     // 兼容旧配置: 方舟 ChatModel 接入点 ID
	DefaultArkURL        = "https://ark.cn-beijing.volces.com/api/v3"
	MaxTextForLLM        = 4000 // 发送给大模型的最大字符数
)

var (
	chatModel     *openai.ChatModel
	chatModelOnce sync.Once
	chatModelInit bool
)

func getEnvWithFallback(primary, fallback string) string {
	value := os.Getenv(primary)
	if value != "" && value != "xxxxx" {
		return value
	}
	return os.Getenv(fallback)
}

func arkBaseURL() string {
	baseURL := os.Getenv(EnvArkBaseURL)
	if baseURL == "" {
		return DefaultArkURL
	}
	return strings.TrimRight(baseURL, "/")
}

func initChatModel() {
	chatModelOnce.Do(func() {
		apiKey := os.Getenv(EnvArkAPIKey)
		if apiKey == "" || apiKey == "xxxxx" {
			zap.L().Info("火山方舟 API Key 未配置, 语义识别将使用规则推理降级方案",
				zap.String("env_var", EnvArkAPIKey))
			return
		}
		baseURL := arkBaseURL()
		endpointID := getEnvWithFallback(EnvArkChatModel, EnvArkEndpointID)
		if endpointID == "" || endpointID == "xxxxx" {
			zap.L().Error("火山方舟 ChatModel 未配置, 请设置 ARK_CHAT_MODEL 或 ARK_ENDPOINT_ID",
				zap.String("env_var", EnvArkChatModel))
			return
		}

		var err error
		chatModel, err = openai.NewChatModel(context.Background(), &openai.ChatModelConfig{
			BaseURL: baseURL,
			APIKey:  apiKey,
			Model:   endpointID, // 方舟使用 endpoint ID 作为 model 参数
		})
		if err != nil {
			zap.L().Error("火山方舟 ChatModel 初始化失败", zap.Error(err))
			return
		}
		chatModelInit = true
		zap.L().Info("火山方舟 ChatModel 初始化成功",
			zap.String("endpoint_id", endpointID),
			zap.String("base_url", baseURL))
	})
}

// IsLLMReady 返回大模型是否已就绪
func IsLLMReady() bool {
	initChatModel()
	return chatModelInit
}

func AnalyzeSemantic(text, sensitiveType, riskLevel string) SemanticResult {
	initChatModel()

	var result SemanticResult
	if chatModelInit && chatModel != nil {
		llmResult, err := analyzeWithLLM(text, sensitiveType, riskLevel)
		if err == nil {
			result = llmResult
		} else {
			zap.L().Warn("大模型语义识别失败, 降级到规则推理", zap.Error(err))
		}
	}
	if result.SensitiveType == "" {
		result = analyzeWithRules(text, sensitiveType, riskLevel)
	}
	attachEmbedding(text, &result)
	return result
}

func analyzeWithLLM(text, sensitiveType, riskLevel string) (SemanticResult, error) {
	truncated := text
	if len([]rune(truncated)) > MaxTextForLLM {
		truncated = string([]rune(truncated)[:MaxTextForLLM])
	}

	prompt := buildSemanticPrompt(sensitiveType, riskLevel, truncated)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	msg, err := chatModel.Generate(ctx, []*schema.Message{
		{Role: schema.System, Content: "你是一个专业的敏感文件识别助手, 负责分析文档内容并判断其敏感级别. 请只返回 JSON, 不要包含其他文字."},
		{Role: schema.User, Content: prompt},
	})
	if err != nil {
		return SemanticResult{}, err
	}

	return parseLLMResponse(msg.Content, sensitiveType, riskLevel)
}

func buildSemanticPrompt(sensitiveType, riskLevel, text string) string {
	replacer := strings.NewReplacer(
		"{{SENSITIVE_TYPE}}", sensitiveType,
		"{{RISK_LEVEL}}", riskLevel,
		"{{DOCUMENT_TEXT}}", text,
	)
	return replacer.Replace(semanticPromptTemplate)
}

func parseLLMResponse(content, sensitiveType, riskLevel string) (SemanticResult, error) {
	content = strings.TrimSpace(content)
	// 去掉 markdown 代码块包裹
	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	} else if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	}

	var resp struct {
		SemanticLabels []string `json:"semantic_labels"`
		SensitiveType  string   `json:"sensitive_type"`
		RiskLevel      string   `json:"risk_level"`
		Explanation    string   `json:"explanation"`
	}
	if err := json.Unmarshal([]byte(content), &resp); err != nil {
		return SemanticResult{}, fmt.Errorf("解析大模型返回 JSON 失败: %w", err)
	}

	if resp.SensitiveType == "" {
		resp.SensitiveType = sensitiveType
	}
	if resp.SensitiveType == "" {
		resp.SensitiveType = "未分类敏感文件"
	}
	if resp.RiskLevel == "" {
		resp.RiskLevel = riskLevel
	}
	if resp.RiskLevel == "" {
		resp.RiskLevel = "medium"
	}
	if len(resp.SemanticLabels) == 0 {
		resp.SemanticLabels = []string{resp.SensitiveType}
	}

	return SemanticResult{
		SemanticLabels: resp.SemanticLabels,
		SensitiveType:  resp.SensitiveType,
		RiskLevel:      resp.RiskLevel,
		EmbeddingID:    "",
		ModelName:      os.Getenv(EnvArkEndpointID),
		Explanation:    resp.Explanation,
	}, nil
}

func attachEmbedding(text string, result *SemanticResult) {
	vector, modelName, err := GenerateEmbedding(text)
	if err != nil {
		if err != errEmbeddingNotConfigured {
			zap.L().Warn("生成语义向量失败", zap.Error(err))
		}
		return
	}
	result.Embedding = vector
	result.EmbeddingID = embeddingID(modelName, text)
}

var errEmbeddingNotConfigured = fmt.Errorf("embedding model not configured")

func GenerateEmbedding(text string) ([]float64, string, error) {
	apiKey := os.Getenv(EnvArkAPIKey)
	modelName := os.Getenv(EnvArkEmbeddingModel)
	if apiKey == "" || apiKey == "xxxxx" || modelName == "" || modelName == "xxxxx" {
		return nil, "", errEmbeddingNotConfigured
	}
	truncated := text
	if len([]rune(truncated)) > MaxTextForLLM {
		truncated = string([]rune(truncated)[:MaxTextForLLM])
	}

	payload := map[string]any{
		"model": modelName,
		"input": truncated,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, modelName, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, arkBaseURL()+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, modelName, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, modelName, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, modelName, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, modelName, fmt.Errorf("embedding request failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var parsed struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, modelName, err
	}
	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return nil, modelName, fmt.Errorf("embedding response missing vector")
	}
	return parsed.Data[0].Embedding, modelName, nil
}

func embeddingID(modelName, text string) string {
	sum := sha256.Sum256([]byte(modelName + "\x00" + text))
	return "emb_" + hex.EncodeToString(sum[:8])
}

// 规则推理降级方案
func analyzeWithRules(text, sensitiveType, riskLevel string) SemanticResult {
	labels := inferLabels(text + " " + sensitiveType)
	if sensitiveType == "" && len(labels) > 0 {
		sensitiveType = strings.Join(labels, "/")
	}
	if sensitiveType == "" {
		sensitiveType = "未分类敏感文件"
	}
	if riskLevel == "" {
		riskLevel = inferRisk(labels)
	}
	if len(labels) == 0 {
		labels = []string{sensitiveType}
	}
	return SemanticResult{
		SemanticLabels: labels,
		SensitiveType:  sensitiveType,
		RiskLevel:      riskLevel,
		EmbeddingID:    "",
		ModelName:      "rule-fallback",
		Explanation:    "文档命中敏感样本规则, 包含" + strings.Join(labels, "/") + "等敏感语义或业务关键词.",
	}
}

func inferLabels(text string) []string {
	candidates := map[string][]string{
		"客户名单":   {"客户", "联系人", "客户名称", "名单"},
		"报价信息":   {"报价", "合同金额", "万元", "价格"},
		"财务预算":   {"财务", "预算", "成本", "利润", "财报"},
		"薪资明细":   {"薪资", "工资", "奖金", "绩效"},
		"保密协议":   {"保密", "协议", "不得披露"},
		"研发设计文档": {"研发", "设计", "架构", "技术方案"},
		"源代码说明":  {"源代码", "接口", "函数", "数据库连接"},
		"运维账号":   {"账号", "密码", "token", "secret", "运维"},
		"安全漏洞信息": {"漏洞", "CVE", "修复", "攻击"},
		"战略规划":   {"战略", "规划", "商业计划", "未公开"},
	}
	var labels []string
	for label, words := range candidates {
		for _, word := range words {
			if strings.Contains(strings.ToLower(text), strings.ToLower(word)) {
				labels = append(labels, label)
				break
			}
		}
	}
	return labels
}

func inferRisk(labels []string) string {
	for _, label := range labels {
		if label == "运维账号" || label == "安全漏洞信息" || label == "客户名单" || label == "报价信息" {
			return "high"
		}
	}
	if len(labels) > 0 {
		return "medium"
	}
	return "low"
}
