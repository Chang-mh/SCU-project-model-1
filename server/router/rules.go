package router

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"scu-project-model-1/server/core"
	"scu-project-model-1/server/dal"
	"scu-project-model-1/server/model"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type SampleUploadResponse struct {
	SensitiveFileID     string          `json:"sensitive_file_id"`
	FileName            string          `json:"file_name"`
	SensitiveType       string          `json:"sensitive_type"`
	RiskLevel           string          `json:"risk_level"`
	RuleVersion         int             `json:"rule_version"`
	GeneratedRulesCount int             `json:"generated_rules_count"`
	GeneratedRules      []core.RuleData `json:"generated_rules"`
	EmbeddingID         string          `json:"embedding_id"`
	SemanticLabels      []string        `json:"semantic_labels"`
	Fingerprint         map[string]any  `json:"fingerprint"`
	Explanation         string          `json:"explanation"`
}

type RuleSyncResponse struct {
	LatestVersion  int            `json:"latest_version"`
	FullSync       bool           `json:"full_sync"`
	Rules          []RuleResp     `json:"rules"`
	DeletedRuleIDs []string       `json:"deleted_rule_ids"`
	Fingerprints   []FingerResp   `json:"fingerprints"`
	SemanticLabels []SemanticResp `json:"semantic_labels"`
	Config         SyncConfig     `json:"config"`
}

type SyncConfig struct {
	SimHashThreshold   int                 `json:"simhash_threshold"`
	SemanticLabelHints map[string][]string `json:"semantic_label_hints"`
}

type RuleResp struct {
	RuleID        string         `json:"rule_id"`
	RuleType      string         `json:"rule_type"`
	SensitiveType string         `json:"sensitive_type"`
	RiskLevel     string         `json:"risk_level"`
	Source        string         `json:"source"`
	Content       map[string]any `json:"content"`
}

type FingerResp struct {
	SensitiveFileID string `json:"sensitive_file_id"`
	SHA256          string `json:"sha256"`
	SimHash         string `json:"simhash"`
}

type SemanticResp struct {
	SensitiveFileID string   `json:"sensitive_file_id"`
	SemanticLabels  []string `json:"semantic_labels"`
	EmbeddingID     string   `json:"embedding_id"`
	ModelName       string   `json:"model_name"`
}

type SemanticSearchResult struct {
	SensitiveFileID string   `json:"sensitive_file_id"`
	FileName        string   `json:"file_name"`
	SensitiveType   string   `json:"sensitive_type"`
	RiskLevel       string   `json:"risk_level"`
	SemanticLabels  []string `json:"semantic_labels"`
	EmbeddingID     string   `json:"embedding_id"`
	ModelName       string   `json:"model_name"`
	Score           float64  `json:"score"`
}

func buildSemanticResps(features []model.SemanticFeature) []SemanticResp {
	resps := make([]SemanticResp, 0, len(features))
	for _, feature := range features {
		var labels []string
		if feature.SemanticLabels != "" {
			if err := json.Unmarshal([]byte(feature.SemanticLabels), &labels); err != nil {
				zap.L().Warn("解析语义标签失败", zap.String("sample_id", feature.SampleID), zap.Error(err))
			}
		}
		resps = append(resps, SemanticResp{
			SensitiveFileID: feature.SampleID,
			SemanticLabels:  labels,
			EmbeddingID:     feature.EmbeddingID,
			ModelName:       feature.ModelName,
		})
	}
	return resps
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		zap.L().Warn("生成随机ID失败，使用时间戳降级", zap.Error(err))
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func genFileID() string {
	return "file_" + randomHex(16)
}

func genRuleID() string {
	return "rule_" + randomHex(12)
}

func genSemanticID() string {
	return "sem_" + randomHex(12)
}

const (
	maxUploadFileSize = 50 * 1024 * 1024
	maxBatchFileCount = 50
	maxBatchTotalSize = 200 * 1024 * 1024
	maxFileNameLength = 255
)

func simhashThresholdFromEnv() int {
	const defaultThreshold = 3
	value := strings.TrimSpace(os.Getenv("SIMHASH_THRESHOLD"))
	if value == "" {
		return defaultThreshold
	}
	threshold, err := strconv.Atoi(value)
	if err != nil || threshold < 0 || threshold > 16 {
		zap.L().Warn("SIMHASH_THRESHOLD 无效，使用默认值", zap.String("value", value), zap.Int("default", defaultThreshold))
		return defaultThreshold
	}
	return threshold
}

func syncConfig() SyncConfig {
	return SyncConfig{SimHashThreshold: simhashThresholdFromEnv(), SemanticLabelHints: core.SemanticLabelHints}
}

func normalizeRiskLevel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical", "high", "medium", "low", "info":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "medium"
	}
}

func validateUploadName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("文件名不能为空")
	}
	if len([]rune(name)) > maxFileNameLength {
		return fmt.Errorf("文件名长度超过限制: %d", maxFileNameLength)
	}
	cleanName := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	if cleanName == "." || strings.HasPrefix(cleanName, "../") || strings.Contains(cleanName, "/../") || path.IsAbs(cleanName) {
		return fmt.Errorf("文件名包含非法路径: %s", name)
	}
	return nil
}

func readUploadFileLimited(fh *multipart.FileHeader, limit int64) ([]byte, error) {
	if err := validateUploadName(fh.Filename); err != nil {
		return nil, err
	}
	file, err := fh.Open()
	if err != nil {
		return nil, fmt.Errorf("无法读取上传文件: %w", err)
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("文件大小超过限制: %d bytes", limit)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("文件内容不能为空")
	}
	return data, nil
}

type preparedUpload struct {
	FileID        string
	FileName      string
	Text          string
	SHA256        string
	SimHash       string
	Rules         []core.RuleData
	Semantic      core.SemanticResult
	ParseWarn     error
	OriginalBytes int
}

type duplicateUpload struct {
	FileName        string `json:"file_name"`
	SHA256          string `json:"sha256"`
	Reason          string `json:"reason"`
	SensitiveFileID string `json:"sensitive_file_id,omitempty"`
	ExistingName    string `json:"existing_file_name,omitempty"`
	UploadedAt      string `json:"uploaded_at,omitempty"`
}

func findExistingSampleBySHA(db *gorm.DB, sha256 string) (*model.SensitiveSample, bool, error) {
	var sample model.SensitiveSample
	err := db.Where("sha256 = ?", sha256).First(&sample).Error
	if err == nil {
		return &sample, true, nil
	}
	if err == gorm.ErrRecordNotFound {
		return nil, false, nil
	}
	return nil, false, err
}

func detectDuplicateUploads(items []preparedUpload) ([]duplicateUpload, error) {
	seen := make(map[string]string)
	duplicates := make([]duplicateUpload, 0)
	for _, item := range items {
		if firstName, ok := seen[item.SHA256]; ok {
			duplicates = append(duplicates, duplicateUpload{FileName: item.FileName, SHA256: item.SHA256, Reason: "request_duplicate", ExistingName: firstName})
			continue
		}
		seen[item.SHA256] = item.FileName

		existing, found, err := findExistingSampleBySHA(dal.DB, item.SHA256)
		if err != nil {
			return nil, err
		}
		if found {
			duplicates = append(duplicates, duplicateUpload{
				FileName:        item.FileName,
				SHA256:          item.SHA256,
				Reason:          "existing_sample",
				SensitiveFileID: existing.ID,
				ExistingName:    existing.FileName,
				UploadedAt:      existing.UploadedAt.Format(time.RFC3339),
			})
		}
	}
	return duplicates, nil
}

func writeDuplicateResponse(ctx *app.RequestContext, duplicates []duplicateUpload) {
	ctx.JSON(consts.StatusConflict, map[string]any{
		"error":      "上传文件已存在于敏感文件库或当前请求中",
		"duplicates": duplicates,
	})
}

func prepareUpload(fileName string, data []byte, sensitiveType, riskLevel, description string) preparedUpload {
	text, _, err := core.ExtractText(fileName, data)
	if err != nil {
		zap.L().Warn("文本解析不完整", zap.String("file", fileName), zap.Error(err))
	}
	if text == "" {
		text = fmt.Sprintf("[二进制文件: %s, 大小: %d bytes]", fileName, len(data))
	}

	sha256 := core.SHA256Hex(data)
	return preparedUpload{
		FileID:        genFileID(),
		FileName:      fileName,
		Text:          text,
		SHA256:        sha256,
		SimHash:       core.SimHashString(text),
		Rules:         core.GenerateRules(text, sensitiveType, riskLevel, description),
		Semantic:      core.AnalyzeSemantic(text, sensitiveType, riskLevel),
		ParseWarn:     err,
		OriginalBytes: len(data),
	}
}

func createRuleVersion(tx *gorm.DB, changeType string) (int, error) {
	ver := model.RuleVersion{ChangeType: changeType, CreatedAt: time.Now()}
	if err := tx.Create(&ver).Error; err != nil {
		return 0, fmt.Errorf("保存规则版本失败: %w", err)
	}
	return ver.Version, nil
}

func saveSemanticFeature(tx *gorm.DB, sampleID string, version int, semantic core.SemanticResult) error {
	labels, err := json.Marshal(semantic.SemanticLabels)
	if err != nil {
		return fmt.Errorf("序列化语义标签失败: %w", err)
	}
	embedding := "[]"
	if len(semantic.Embedding) > 0 {
		data, err := json.Marshal(semantic.Embedding)
		if err != nil {
			return fmt.Errorf("序列化语义向量失败: %w", err)
		}
		embedding = string(data)
	}
	feature := model.SemanticFeature{
		ID:             genSemanticID(),
		SampleID:       sampleID,
		Version:        version,
		SemanticLabels: string(labels),
		EmbeddingID:    semantic.EmbeddingID,
		Embedding:      embedding,
		ModelName:      semantic.ModelName,
		CreatedAt:      time.Now(),
	}
	if feature.ModelName == "" {
		feature.ModelName = "rule-fallback"
	}
	if err := tx.Create(&feature).Error; err != nil {
		return fmt.Errorf("保存语义特征失败: %w", err)
	}
	return nil
}

func persistPreparedUpload(tx *gorm.DB, item preparedUpload, version int) error {
	sample := model.SensitiveSample{
		ID:            item.FileID,
		FileName:      item.FileName,
		FileType:      filepath.Ext(item.FileName),
		SensitiveType: item.Semantic.SensitiveType,
		RiskLevel:     item.Semantic.RiskLevel,
		SHA256:        item.SHA256,
		Explanation:   item.Semantic.Explanation,
		ExtractedText: item.Text,
		UploadedAt:    time.Now(),
	}
	if err := tx.Create(&sample).Error; err != nil {
		return fmt.Errorf("保存样本失败: %w", err)
	}

	for _, rule := range item.Rules {
		r := model.GeneratedRule{
			ID:            genRuleID(),
			SampleID:      item.FileID,
			Version:       version,
			RuleType:      rule.RuleType,
			SensitiveType: rule.SensitiveType,
			RiskLevel:     rule.RiskLevel,
			Content:       core.RuleContentJSON(rule.Content),
			Source:        "sample",
			Enabled:       true,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
		if err := tx.Create(&r).Error; err != nil {
			return fmt.Errorf("保存规则失败: %w", err)
		}
	}

	fp := model.FileFingerprint{
		SampleID:   item.FileID,
		Version:    version,
		SHA256:     item.SHA256,
		SimHash:    item.SimHash,
		TextLength: len(item.Text),
	}
	if err := tx.Create(&fp).Error; err != nil {
		return fmt.Errorf("保存指纹失败: %w", err)
	}
	if err := saveSemanticFeature(tx, item.FileID, version, item.Semantic); err != nil {
		return err
	}
	return nil
}

func sampleUploadResponse(item preparedUpload, version int) SampleUploadResponse {
	return SampleUploadResponse{
		SensitiveFileID:     item.FileID,
		FileName:            item.FileName,
		SensitiveType:       item.Semantic.SensitiveType,
		RiskLevel:           item.Semantic.RiskLevel,
		RuleVersion:         version,
		GeneratedRulesCount: len(item.Rules),
		GeneratedRules:      item.Rules,
		EmbeddingID:         item.Semantic.EmbeddingID,
		SemanticLabels:      item.Semantic.SemanticLabels,
		Fingerprint:         map[string]any{"sha256": item.SHA256, "simhash": item.SimHash},
		Explanation:         item.Semantic.Explanation,
	}
}

func UploadSample(_ context.Context, ctx *app.RequestContext) {
	fileHeader, err := ctx.FormFile("file")
	if err != nil {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "缺少文件字段"})
		return
	}

	data, err := readUploadFileLimited(fileHeader, maxUploadFileSize)
	if err != nil {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	sensitiveType := string(ctx.FormValue("sensitive_type"))
	riskLevel := normalizeRiskLevel(string(ctx.FormValue("risk_level")))
	description := string(ctx.FormValue("description"))

	item := prepareUpload(fileHeader.Filename, data, sensitiveType, riskLevel, description)
	duplicates, err := detectDuplicateUploads([]preparedUpload{item})
	if err != nil {
		zap.L().Error("检查重复样本失败", zap.String("file", fileHeader.Filename), zap.Error(err))
		ctx.JSON(consts.StatusInternalServerError, map[string]string{"error": "检查重复样本失败"})
		return
	}
	if len(duplicates) > 0 {
		writeDuplicateResponse(ctx, duplicates)
		return
	}

	var newVersion int
	if err := dal.DB.Transaction(func(tx *gorm.DB) error {
		version, err := createRuleVersion(tx, "upload")
		if err != nil {
			return err
		}
		newVersion = version
		if err := persistPreparedUpload(tx, item, newVersion); err != nil {
			return err
		}
		return nil
	}); err != nil {
		zap.L().Error("上传样本事务失败", zap.String("file", fileHeader.Filename), zap.Error(err))
		ctx.JSON(consts.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	resp := sampleUploadResponse(item, newVersion)

	zap.L().Info("样本上传成功",
		zap.String("file_id", item.FileID),
		zap.String("file", item.FileName),
		zap.Int("rules", len(item.Rules)),
		zap.Int("version", newVersion),
	)
	ctx.JSON(consts.StatusOK, resp)
}

func buildRuleSyncResponse(clientVersion int) (RuleSyncResponse, error) {
	latestVersion := 0
	var latest model.RuleVersion
	if err := dal.DB.Order("version desc").First(&latest).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			return RuleSyncResponse{}, fmt.Errorf("查询最新规则版本失败: %w", err)
		}
	} else {
		latestVersion = latest.Version
	}

	var rules []model.GeneratedRule
	ruleQuery := dal.DB.Where("source = ? OR version > ?", core.BuiltinRuleSource, clientVersion)
	if err := ruleQuery.Find(&rules).Error; err != nil {
		return RuleSyncResponse{}, fmt.Errorf("查询规则失败: %w", err)
	}

	var fingerprints []model.FileFingerprint
	fingerprintQuery := dal.DB
	if clientVersion != 0 {
		fingerprintQuery = fingerprintQuery.Where("version > ?", clientVersion)
	}
	if err := fingerprintQuery.Find(&fingerprints).Error; err != nil {
		return RuleSyncResponse{}, fmt.Errorf("查询指纹失败: %w", err)
	}

	var semanticFeatures []model.SemanticFeature
	semanticQuery := dal.DB
	if clientVersion != 0 {
		semanticQuery = semanticQuery.Where("version > ?", clientVersion)
	}
	if err := semanticQuery.Find(&semanticFeatures).Error; err != nil {
		return RuleSyncResponse{}, fmt.Errorf("查询语义特征失败: %w", err)
	}

	ruleResps := make([]RuleResp, 0, len(rules))
	deletedRuleIDs := make([]string, 0)
	for _, r := range rules {
		if !r.Enabled || r.DeletedAt != nil {
			if r.Version > clientVersion {
				deletedRuleIDs = append(deletedRuleIDs, r.ID)
			}
			continue
		}
		content := map[string]any{}
		if err := json.Unmarshal([]byte(r.Content), &content); err != nil {
			zap.L().Warn("解析规则内容失败", zap.String("rule_id", r.ID), zap.Error(err))
		}
		ruleResps = append(ruleResps, RuleResp{
			RuleID:        r.ID,
			RuleType:      r.RuleType,
			SensitiveType: r.SensitiveType,
			RiskLevel:     r.RiskLevel,
			Source:        r.Source,
			Content:       content,
		})
	}

	fingerResps := make([]FingerResp, 0, len(fingerprints))
	for _, f := range fingerprints {
		fingerResps = append(fingerResps, FingerResp{
			SensitiveFileID: f.SampleID,
			SHA256:          f.SHA256,
			SimHash:         f.SimHash,
		})
	}

	return RuleSyncResponse{
		LatestVersion:  latestVersion,
		FullSync:       clientVersion == 0,
		Rules:          ruleResps,
		DeletedRuleIDs: deletedRuleIDs,
		Fingerprints:   fingerResps,
		SemanticLabels: buildSemanticResps(semanticFeatures),
		Config:         syncConfig(),
	}, nil
}

func SyncRules(_ context.Context, ctx *app.RequestContext) {
	versionStr := ctx.Query("version")
	if versionStr == "" {
		versionStr = "0"
	}
	clientVersion, err := strconv.Atoi(versionStr)
	if err != nil {
		clientVersion = 0
	}

	resp, err := buildRuleSyncResponse(clientVersion)
	if err != nil {
		zap.L().Error("同步规则失败", zap.Error(err))
		ctx.JSON(consts.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	ctx.JSON(consts.StatusOK, resp)
}

func UpdateRule(_ context.Context, ctx *app.RequestContext) {
	ruleID := strings.TrimSpace(ctx.Param("rule_id"))
	if ruleID == "" {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "rule_id 不能为空"})
		return
	}

	var req struct {
		SensitiveType string         `json:"sensitive_type"`
		RiskLevel     string         `json:"risk_level"`
		Enabled       *bool          `json:"enabled"`
		Content       map[string]any `json:"content"`
	}
	if err := json.Unmarshal(ctx.Request.Body(), &req); err != nil {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}

	var updated model.GeneratedRule
	if err := dal.DB.Transaction(func(tx *gorm.DB) error {
		var rule model.GeneratedRule
		if err := tx.Where("id = ?", ruleID).First(&rule).Error; err != nil {
			return err
		}
		version, err := createRuleVersion(tx, "rule_update")
		if err != nil {
			return err
		}
		updates := map[string]any{"version": version, "updated_at": time.Now()}
		if strings.TrimSpace(req.SensitiveType) != "" {
			updates["sensitive_type"] = strings.TrimSpace(req.SensitiveType)
		}
		if strings.TrimSpace(req.RiskLevel) != "" {
			updates["risk_level"] = normalizeRiskLevel(req.RiskLevel)
		}
		if req.Enabled != nil {
			updates["enabled"] = *req.Enabled
			if *req.Enabled {
				updates["deleted_at"] = nil
			}
		}
		if req.Content != nil {
			updates["content"] = core.RuleContentJSON(req.Content)
		}
		if err := tx.Model(&rule).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", ruleID).First(&updated).Error
	}); err != nil {
		if err == gorm.ErrRecordNotFound {
			ctx.JSON(consts.StatusNotFound, map[string]string{"error": "规则不存在"})
		} else {
			zap.L().Error("更新规则失败", zap.String("rule_id", ruleID), zap.Error(err))
			ctx.JSON(consts.StatusInternalServerError, map[string]string{"error": "更新规则失败"})
		}
		return
	}

	content := map[string]any{}
	if err := json.Unmarshal([]byte(updated.Content), &content); err != nil {
		zap.L().Warn("解析更新后规则内容失败", zap.String("rule_id", updated.ID), zap.Error(err))
	}
	ctx.JSON(consts.StatusOK, RuleResp{
		RuleID:        updated.ID,
		RuleType:      updated.RuleType,
		SensitiveType: updated.SensitiveType,
		RiskLevel:     updated.RiskLevel,
		Source:        updated.Source,
		Content:       content,
	})
}

func DeleteRule(_ context.Context, ctx *app.RequestContext) {
	ruleID := strings.TrimSpace(ctx.Param("rule_id"))
	if ruleID == "" {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "rule_id 不能为空"})
		return
	}

	deletedAt := time.Now()
	var version int
	if err := dal.DB.Transaction(func(tx *gorm.DB) error {
		var rule model.GeneratedRule
		if err := tx.Where("id = ?", ruleID).First(&rule).Error; err != nil {
			return err
		}
		newVersion, err := createRuleVersion(tx, "rule_delete")
		if err != nil {
			return err
		}
		version = newVersion
		return tx.Model(&rule).Updates(map[string]any{
			"enabled":    false,
			"deleted_at": &deletedAt,
			"version":    newVersion,
			"updated_at": deletedAt,
		}).Error
	}); err != nil {
		if err == gorm.ErrRecordNotFound {
			ctx.JSON(consts.StatusNotFound, map[string]string{"error": "规则不存在"})
		} else {
			zap.L().Error("删除规则失败", zap.String("rule_id", ruleID), zap.Error(err))
			ctx.JSON(consts.StatusInternalServerError, map[string]string{"error": "删除规则失败"})
		}
		return
	}

	ctx.JSON(consts.StatusOK, map[string]any{"rule_id": ruleID, "deleted": true, "rule_version": version})
}

func GetSensitiveFileInfo(_ context.Context, ctx *app.RequestContext) {
	fileHash := ctx.Param("file_hash")
	var sample model.SensitiveSample
	if err := dal.DB.Where("sha256 = ?", fileHash).First(&sample).Error; err != nil {
		ctx.JSON(consts.StatusNotFound, map[string]string{"error": "未找到敏感文件"})
		return
	}
	ctx.JSON(consts.StatusOK, map[string]any{
		"sensitive_file_id": sample.ID,
		"file_name":         sample.FileName,
		"sensitive_type":    sample.SensitiveType,
		"risk_level":        sample.RiskLevel,
		"sha256":            sample.SHA256,
		"explanation":       sample.Explanation,
	})
}

func ListSamples(_ context.Context, ctx *app.RequestContext) {
	var samples []model.SensitiveSample
	dal.DB.Order("uploaded_at desc").Limit(50).Find(&samples)
	ctx.JSON(consts.StatusOK, samples)
}

func QuerySensitiveFile(_ context.Context, ctx *app.RequestContext) {
	var query struct {
		FileHash string `json:"file_hash"`
		FilePath string `json:"file_path"`
		FileName string `json:"file_name"`
	}
	if err := json.Unmarshal(ctx.Request.Body(), &query); err != nil {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}

	fileHash := strings.TrimSpace(query.FileHash)
	fileName := strings.TrimSpace(query.FileName)
	filePath := strings.TrimSpace(query.FilePath)
	matchType := "sha256"
	if fileHash == "" {
		if fileName != "" {
			matchType = "filename_like"
		} else if filePath != "" {
			fileName = filepath.Base(filePath)
			matchType = "filepath_basename_like"
		}
	}
	if fileHash == "" && fileName == "" {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "至少需要提供 file_hash 或 file_name"})
		return
	}

	var sample model.SensitiveSample
	dbQuery := dal.DB
	if fileHash != "" {
		dbQuery = dbQuery.Where("sha256 = ?", fileHash)
	} else {
		dbQuery = dbQuery.Where("file_name LIKE ?", "%"+fileName+"%")
	}

	if err := dbQuery.First(&sample).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			ctx.JSON(consts.StatusOK, map[string]any{"sensitive": false})
		} else {
			ctx.JSON(consts.StatusInternalServerError, map[string]string{"error": "查询失败"})
		}
		return
	}

	ctx.JSON(consts.StatusOK, map[string]any{
		"sensitive":         true,
		"match_type":        matchType,
		"sensitive_file_id": sample.ID,
		"file_name":         sample.FileName,
		"sensitive_type":    sample.SensitiveType,
		"risk_level":        sample.RiskLevel,
	})
}

func UploadSamplesBatch(_ context.Context, ctx *app.RequestContext) {
	form, err := ctx.MultipartForm()
	if err != nil {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "缺少文件字段"})
		return
	}
	files := form.File["files"]
	if len(files) == 0 {
		files = form.File["file"]
	}
	if len(files) == 0 {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "请上传至少一个文件"})
		return
	}

	sensitiveType := string(ctx.FormValue("sensitive_type"))
	riskLevel := normalizeRiskLevel(string(ctx.FormValue("risk_level")))
	description := string(ctx.FormValue("description"))

	if len(files) > maxBatchFileCount {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": fmt.Sprintf("批量上传文件数量超过限制: %d", maxBatchFileCount)})
		return
	}

	items := make([]preparedUpload, 0, len(files))
	var totalSize int64
	for _, fh := range files {
		data, err := readUploadFileLimited(fh, maxUploadFileSize)
		if err != nil {
			ctx.JSON(consts.StatusBadRequest, map[string]string{"error": fmt.Sprintf("文件 %s 校验失败: %v", fh.Filename, err)})
			return
		}
		totalSize += int64(len(data))
		if totalSize > maxBatchTotalSize {
			ctx.JSON(consts.StatusBadRequest, map[string]string{"error": fmt.Sprintf("批量上传总大小超过限制: %d bytes", maxBatchTotalSize)})
			return
		}
		items = append(items, prepareUpload(fh.Filename, data, sensitiveType, riskLevel, description))
	}

	duplicates, err := detectDuplicateUploads(items)
	if err != nil {
		zap.L().Error("检查批量上传重复样本失败", zap.Error(err))
		ctx.JSON(consts.StatusInternalServerError, map[string]string{"error": "检查重复样本失败"})
		return
	}
	if len(duplicates) > 0 {
		writeDuplicateResponse(ctx, duplicates)
		return
	}

	results := make([]SampleUploadResponse, 0, len(items))
	var newVersion int
	if err := dal.DB.Transaction(func(tx *gorm.DB) error {
		version, err := createRuleVersion(tx, "batch_upload")
		if err != nil {
			return err
		}
		newVersion = version
		for _, item := range items {
			if err := persistPreparedUpload(tx, item, newVersion); err != nil {
				return fmt.Errorf("文件 %s 入库失败: %w", item.FileName, err)
			}
		}
		return nil
	}); err != nil {
		zap.L().Error("批量上传事务失败", zap.Error(err))
		ctx.JSON(consts.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	for _, item := range items {
		results = append(results, sampleUploadResponse(item, newVersion))
	}

	resp := map[string]any{
		"total":   len(results),
		"results": results,
	}

	ctx.JSON(consts.StatusOK, resp)
}

const (
	maxZipFileCount       = 200
	maxZipEntrySize       = 20 * 1024 * 1024
	maxZipTotalSize       = 100 * 1024 * 1024
	maxZipReadBufferExtra = 1 * 1024
)

func parseZip(data []byte) (map[string][]byte, error) {
	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := make(map[string][]byte)
	var totalSize uint64
	for _, f := range zipReader.File {
		if f.FileInfo().IsDir() {
			continue
		}
		cleanName := path.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		if cleanName == "." || strings.HasPrefix(cleanName, "../") || strings.Contains(cleanName, "/../") || path.IsAbs(cleanName) {
			return nil, fmt.Errorf("ZIP包含非法路径: %s", f.Name)
		}
		if strings.EqualFold(filepath.Ext(cleanName), ".zip") {
			return nil, fmt.Errorf("暂不支持嵌套ZIP文件: %s", f.Name)
		}
		if len(files) >= maxZipFileCount {
			return nil, fmt.Errorf("ZIP文件数量超过限制: %d", maxZipFileCount)
		}
		if f.UncompressedSize64 > maxZipEntrySize {
			return nil, fmt.Errorf("ZIP内文件过大: %s", f.Name)
		}
		totalSize += f.UncompressedSize64
		if totalSize > maxZipTotalSize {
			return nil, fmt.Errorf("ZIP解压总大小超过限制: %d bytes", maxZipTotalSize)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("打开ZIP内文件失败 %s: %w", f.Name, err)
		}
		content, err := io.ReadAll(io.LimitReader(rc, maxZipEntrySize+maxZipReadBufferExtra))
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("读取ZIP内文件失败 %s: %w", f.Name, err)
		}
		if len(content) > maxZipEntrySize {
			return nil, fmt.Errorf("ZIP内文件读取超过限制: %s", f.Name)
		}
		files[cleanName] = content
	}
	return files, nil
}

func GetSensitiveFilesList(_ context.Context, ctx *app.RequestContext) {
	keyword := ctx.Query("keyword")
	sensitiveType := ctx.Query("sensitive_type")
	riskLevel := ctx.Query("risk_level")

	var samples []model.SensitiveSample
	dbQuery := dal.DB.Model(&model.SensitiveSample{})
	if keyword != "" {
		dbQuery = dbQuery.Where("file_name LIKE ? OR extracted_text LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	if sensitiveType != "" {
		dbQuery = dbQuery.Where("sensitive_type LIKE ?", "%"+sensitiveType+"%")
	}
	if riskLevel != "" {
		dbQuery = dbQuery.Where("risk_level = ?", riskLevel)
	}
	dbQuery.Order("uploaded_at desc").Limit(100).Find(&samples)
	ctx.JSON(consts.StatusOK, samples)
}

func RegexTest(_ context.Context, ctx *app.RequestContext) {
	var req struct {
		Pattern string `json:"pattern"`
		Text    string `json:"text"`
	}
	if err := json.Unmarshal(ctx.Request.Body(), &req); err != nil {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	re, err := regexp.Compile(req.Pattern)
	if err != nil {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "无效正则表达式: " + err.Error()})
		return
	}
	matches := re.FindAllString(req.Text, -1)
	ctx.JSON(consts.StatusOK, map[string]any{"matches": matches, "count": len(matches)})
}

func FingerprintCompute(_ context.Context, ctx *app.RequestContext) {
	fileHeader, err := ctx.FormFile("file")
	if err != nil {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "缺少文件"})
		return
	}
	data, err := readUploadFileLimited(fileHeader, maxUploadFileSize)
	if err != nil {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	sha256 := core.SHA256Hex(data)
	text, _, _ := core.ExtractText(fileHeader.Filename, data)
	simhash := core.SimHashString(text)
	ctx.JSON(consts.StatusOK, map[string]any{
		"file_name": fileHeader.Filename,
		"sha256":    sha256,
		"simhash":   simhash,
	})
}

func UploadZip(_ context.Context, ctx *app.RequestContext) {
	fileHeader, err := ctx.FormFile("file")
	if err != nil {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "缺少文件"})
		return
	}
	data, err := readUploadFileLimited(fileHeader, maxZipTotalSize)
	if err != nil {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	var files map[string][]byte
	if ext == ".zip" {
		files, err = parseZip(data)
		if err != nil {
			ctx.JSON(consts.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	} else {
		files = map[string][]byte{fileHeader.Filename: data}
	}

	sensitiveType := string(ctx.FormValue("sensitive_type"))
	riskLevel := normalizeRiskLevel(string(ctx.FormValue("risk_level")))
	description := string(ctx.FormValue("description"))

	items := make([]preparedUpload, 0, len(files))
	for name, content := range files {
		items = append(items, prepareUpload(name, content, sensitiveType, riskLevel, description))
	}

	duplicates, err := detectDuplicateUploads(items)
	if err != nil {
		zap.L().Error("检查ZIP上传重复样本失败", zap.Error(err))
		ctx.JSON(consts.StatusInternalServerError, map[string]string{"error": "检查重复样本失败"})
		return
	}
	if len(duplicates) > 0 {
		writeDuplicateResponse(ctx, duplicates)
		return
	}

	var newVersion int
	if err := dal.DB.Transaction(func(tx *gorm.DB) error {
		version, err := createRuleVersion(tx, "zip_upload")
		if err != nil {
			return err
		}
		newVersion = version
		for _, item := range items {
			if err := persistPreparedUpload(tx, item, newVersion); err != nil {
				return fmt.Errorf("文件 %s 入库失败: %w", item.FileName, err)
			}
		}
		return nil
	}); err != nil {
		zap.L().Error("ZIP上传事务失败", zap.String("file", fileHeader.Filename), zap.Error(err))
		ctx.JSON(consts.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	results := make([]SampleUploadResponse, 0, len(items))
	for _, item := range items {
		results = append(results, sampleUploadResponse(item, newVersion))
	}

	ctx.JSON(consts.StatusOK, map[string]any{"total": len(results), "results": results})
}

func BatchSyncRules(_ context.Context, ctx *app.RequestContext) {
	var req struct {
		Rules []RuleSyncQuery `json:"rules"`
	}
	if err := json.Unmarshal(ctx.Request.Body(), &req); err != nil {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	results := make([]map[string]any, 0, len(req.Rules))
	for _, r := range req.Rules {
		resp := getRuleSet(r.Version)
		results = append(results, map[string]any{
			"sensitive_type":   r.SensitiveType,
			"version":          r.Version,
			"latest_version":   resp.LatestVersion,
			"rules":            resp.Rules,
			"deleted_rule_ids": resp.DeletedRuleIDs,
			"fingerprints":     resp.Fingerprints,
			"semantic_labels":  resp.SemanticLabels,
			"config":           resp.Config,
		})
	}
	ctx.JSON(consts.StatusOK, map[string]any{"results": results})
}

type ScanResultReport struct {
	HostID    string      `json:"host_id"`
	ScanPath  string      `json:"scan_path"`
	ScannedAt string      `json:"scanned_at"`
	Results   []ScanEntry `json:"results"`
}

type ScanEntry struct {
	FilePath        string         `json:"file_path"`
	FileHash        string         `json:"file_hash"`
	Sensitive       bool           `json:"sensitive"`
	SensitiveType   string         `json:"sensitive_type"`
	RiskLevel       string         `json:"risk_level"`
	SensitiveFileID string         `json:"sensitive_file_id"`
	MatchScore      int            `json:"match_score"`
	ConfidenceLevel string         `json:"confidence_level"`
	MatchDetail     map[string]any `json:"match_detail"`
}

func ReportScanResults(_ context.Context, ctx *app.RequestContext) {
	var report ScanResultReport
	if err := json.Unmarshal(ctx.Request.Body(), &report); err != nil {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	if strings.TrimSpace(report.HostID) == "" {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "host_id 不能为空"})
		return
	}
	if report.Results == nil {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "results 不能为空"})
		return
	}
	if len(report.Results) > 10000 {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "扫描结果数量超过限制: 10000"})
		return
	}

	reportID := "scan_" + randomHex(16)
	createdAt := time.Now()
	confidenceCounts := map[string]int{}
	if err := dal.DB.Transaction(func(tx *gorm.DB) error {
		scanReport := model.ClientScanReport{
			ID:          reportID,
			HostID:      strings.TrimSpace(report.HostID),
			ScanPath:    report.ScanPath,
			ScannedAt:   report.ScannedAt,
			ResultCount: len(report.Results),
			CreatedAt:   createdAt,
		}
		if err := tx.Create(&scanReport).Error; err != nil {
			return err
		}
		rows := make([]model.ClientScanResult, 0, len(report.Results))
		for _, result := range report.Results {
			confidence := strings.TrimSpace(result.ConfidenceLevel)
			if confidence == "" {
				confidence = "unknown"
			}
			confidenceCounts[confidence]++
			matchDetail := "{}"
			if result.MatchDetail != nil {
				data, err := json.Marshal(result.MatchDetail)
				if err != nil {
					return fmt.Errorf("序列化扫描结果详情失败: %w", err)
				}
				matchDetail = string(data)
			}
			rows = append(rows, model.ClientScanResult{
				ReportID:        reportID,
				FilePath:        result.FilePath,
				FileHash:        result.FileHash,
				Sensitive:       result.Sensitive,
				SensitiveType:   result.SensitiveType,
				RiskLevel:       result.RiskLevel,
				SensitiveFileID: result.SensitiveFileID,
				MatchScore:      result.MatchScore,
				ConfidenceLevel: confidence,
				MatchDetail:     matchDetail,
				CreatedAt:       createdAt,
			})
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.CreateInBatches(rows, 500).Error
	}); err != nil {
		zap.L().Error("保存客户端扫描结果失败", zap.String("host_id", report.HostID), zap.Error(err))
		ctx.JSON(consts.StatusInternalServerError, map[string]string{"error": "保存扫描结果失败"})
		return
	}

	zap.L().Info("收到客户端扫描结果上报",
		zap.String("report_id", reportID),
		zap.String("host_id", report.HostID),
		zap.String("scan_path", report.ScanPath),
		zap.String("scanned_at", report.ScannedAt),
		zap.Int("result_count", len(report.Results)),
		zap.Any("confidence_counts", confidenceCounts),
	)
	ctx.JSON(consts.StatusOK, map[string]any{"status": "received", "report_id": reportID, "received": len(report.Results)})
}

func ListScanResults(_ context.Context, ctx *app.RequestContext) {
	hostID := strings.TrimSpace(ctx.Query("host_id"))
	sensitiveOnly := strings.EqualFold(ctx.Query("sensitive_only"), "true") || ctx.Query("sensitive_only") == "1"
	confidenceMin := strings.TrimSpace(ctx.Query("confidence_min"))
	limit := 200
	if rawLimit := strings.TrimSpace(ctx.Query("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}

	query := dal.DB.Model(&model.ClientScanResult{})
	if hostID != "" {
		query = query.Joins("JOIN client_scan_reports ON client_scan_reports.id = client_scan_results.report_id").
			Where("client_scan_reports.host_id = ?", hostID)
	}
	if sensitiveOnly {
		query = query.Where("client_scan_results.sensitive = ?", true)
	}
	if confidenceMin != "" {
		levels := confidenceFilter(confidenceMin)
		if len(levels) == 0 {
			ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "confidence_min 无效"})
			return
		}
		query = query.Where("client_scan_results.confidence_level IN ?", levels)
	}

	var results []model.ClientScanResult
	if err := query.Order("client_scan_results.created_at desc").Limit(limit).Find(&results).Error; err != nil {
		zap.L().Error("查询客户端扫描结果失败", zap.Error(err))
		ctx.JSON(consts.StatusInternalServerError, map[string]string{"error": "查询扫描结果失败"})
		return
	}
	ctx.JSON(consts.StatusOK, map[string]any{"total": len(results), "results": results})
}

func confidenceFilter(minLevel string) []string {
	order := []string{"clean", "low_confidence", "suspected", "sensitive"}
	minLevel = strings.ToLower(strings.TrimSpace(minLevel))
	for i, level := range order {
		if level == minLevel {
			return order[i:]
		}
	}
	return nil
}

type RuleSyncQuery struct {
	SensitiveType string `json:"sensitive_type"`
	Version       int    `json:"version"`
}

func getRuleSet(clientVersion int) RuleSyncResponse {
	resp, err := buildRuleSyncResponse(clientVersion)
	if err != nil {
		zap.L().Error("批量同步规则失败", zap.Error(err))
		return RuleSyncResponse{LatestVersion: 0}
	}
	return resp
}

func ContentScan(_ context.Context, ctx *app.RequestContext) {
	var req struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(ctx.Request.Body(), &req); err != nil || req.Content == "" {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "缺少内容"})
		return
	}
	results := core.MatchBuiltinRegex(req.Content)
	ctx.JSON(consts.StatusOK, map[string]any{
		"total":   len(results),
		"results": results,
	})
}

func SemanticSearch(_ context.Context, ctx *app.RequestContext) {
	var req struct {
		Content  string  `json:"content"`
		Query    string  `json:"query"`
		TopK     int     `json:"top_k"`
		MinScore float64 `json:"min_score"`
	}
	if err := json.Unmarshal(ctx.Request.Body(), &req); err != nil {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	queryText := strings.TrimSpace(req.Content)
	if queryText == "" {
		queryText = strings.TrimSpace(req.Query)
	}
	if queryText == "" {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "缺少 content 或 query"})
		return
	}
	topK := req.TopK
	if topK <= 0 {
		topK = 5
	}
	if topK > 50 {
		topK = 50
	}

	queryVector, modelName, err := core.GenerateEmbedding(queryText)
	if err != nil {
		zap.L().Warn("语义检索生成查询向量失败", zap.Error(err))
		ctx.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "语义向量模型未就绪或生成失败: " + err.Error()})
		return
	}

	var rows []struct {
		SampleID       string
		EmbeddingID    string
		Embedding      string
		ModelName      string
		SemanticLabels string
		FileName       string
		SensitiveType  string
		RiskLevel      string
	}
	if err := dal.DB.Table("semantic_features").
		Select("semantic_features.sample_id, semantic_features.embedding_id, semantic_features.embedding, semantic_features.model_name, semantic_features.semantic_labels, sensitive_samples.file_name, sensitive_samples.sensitive_type, sensitive_samples.risk_level").
		Joins("LEFT JOIN sensitive_samples ON sensitive_samples.id = semantic_features.sample_id").
		Where("semantic_features.embedding_id <> '' AND semantic_features.embedding <> '' AND semantic_features.embedding <> '[]'").
		Find(&rows).Error; err != nil {
		zap.L().Error("查询语义向量库失败", zap.Error(err))
		ctx.JSON(consts.StatusInternalServerError, map[string]string{"error": "查询语义向量库失败"})
		return
	}

	results := make([]SemanticSearchResult, 0, len(rows))
	for _, row := range rows {
		var vector []float64
		if err := json.Unmarshal([]byte(row.Embedding), &vector); err != nil {
			zap.L().Warn("解析已存语义向量失败", zap.String("sample_id", row.SampleID), zap.Error(err))
			continue
		}
		score, ok := core.CosineSimilarity(queryVector, vector)
		if !ok || score < req.MinScore {
			continue
		}
		var labels []string
		if row.SemanticLabels != "" {
			if err := json.Unmarshal([]byte(row.SemanticLabels), &labels); err != nil {
				zap.L().Warn("解析语义标签失败", zap.String("sample_id", row.SampleID), zap.Error(err))
			}
		}
		results = append(results, SemanticSearchResult{
			SensitiveFileID: row.SampleID,
			FileName:        row.FileName,
			SensitiveType:   row.SensitiveType,
			RiskLevel:       row.RiskLevel,
			SemanticLabels:  labels,
			EmbeddingID:     row.EmbeddingID,
			ModelName:       row.ModelName,
			Score:           score,
		})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if len(results) > topK {
		results = results[:topK]
	}

	ctx.JSON(consts.StatusOK, map[string]any{
		"total":             len(results),
		"embedding_model":   modelName,
		"vector_store":      "semantic_features",
		"similarity_metric": "cosine",
		"results":           results,
	})
}
