package router

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"path"
	"path/filepath"
	"regexp"
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
	Fingerprints   []FingerResp   `json:"fingerprints"`
	SemanticLabels []SemanticResp `json:"semantic_labels"`
}

type RuleResp struct {
	RuleID        string         `json:"rule_id"`
	RuleType      string         `json:"rule_type"`
	SensitiveType string         `json:"sensitive_type"`
	RiskLevel     string         `json:"risk_level"`
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

func saveSemanticFeature(tx *gorm.DB, sampleID string, semantic core.SemanticResult) error {
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
			CreatedAt:     time.Now(),
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
	if err := saveSemanticFeature(tx, item.FileID, item.Semantic); err != nil {
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

func UploadSample(ctx *app.RequestContext) {
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
	var latest model.RuleVersion
	if err := dal.DB.Order("version desc").First(&latest).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return RuleSyncResponse{LatestVersion: 0, FullSync: clientVersion == 0}, nil
		}
		return RuleSyncResponse{}, fmt.Errorf("查询最新规则版本失败: %w", err)
	}

	var rules []model.GeneratedRule
	if err := dal.DB.Where("version > ?", clientVersion).Find(&rules).Error; err != nil {
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
	if err := dal.DB.Find(&semanticFeatures).Error; err != nil {
		return RuleSyncResponse{}, fmt.Errorf("查询语义特征失败: %w", err)
	}

	ruleResps := make([]RuleResp, 0, len(rules))
	for _, r := range rules {
		content := map[string]any{}
		if err := json.Unmarshal([]byte(r.Content), &content); err != nil {
			zap.L().Warn("解析规则内容失败", zap.String("rule_id", r.ID), zap.Error(err))
		}
		ruleResps = append(ruleResps, RuleResp{
			RuleID:        r.ID,
			RuleType:      r.RuleType,
			SensitiveType: r.SensitiveType,
			RiskLevel:     r.RiskLevel,
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
		LatestVersion:  latest.Version,
		FullSync:       clientVersion == 0,
		Rules:          ruleResps,
		Fingerprints:   fingerResps,
		SemanticLabels: buildSemanticResps(semanticFeatures),
	}, nil
}

func SyncRules(ctx *app.RequestContext) {
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

func GetSensitiveFileInfo(ctx *app.RequestContext) {
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

func ListSamples(ctx *app.RequestContext) {
	var samples []model.SensitiveSample
	dal.DB.Order("uploaded_at desc").Limit(50).Find(&samples)
	ctx.JSON(consts.StatusOK, samples)
}

func QuerySensitiveFile(ctx *app.RequestContext) {
	var query struct {
		FileHash string `json:"file_hash"`
		FilePath string `json:"file_path"`
		FileName string `json:"file_name"`
	}
	if err := json.Unmarshal(ctx.Request.Body(), &query); err != nil {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}

	var sample model.SensitiveSample
	dbQuery := dal.DB
	if query.FileHash != "" {
		dbQuery = dbQuery.Where("sha256 = ?", query.FileHash)
	}
	if query.FileName != "" {
		dbQuery = dbQuery.Where("file_name LIKE ?", "%"+query.FileName+"%")
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
		"sensitive_file_id": sample.ID,
		"file_name":         sample.FileName,
		"sensitive_type":    sample.SensitiveType,
		"risk_level":        sample.RiskLevel,
	})
}

func UploadSamplesBatch(ctx *app.RequestContext) {
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

func GetSensitiveFilesList(ctx *app.RequestContext) {
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

func RegexTest(ctx *app.RequestContext) {
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

func FingerprintCompute(ctx *app.RequestContext) {
	fileHeader, err := ctx.FormFile("file")
	if err != nil {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "缺少文件"})
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		ctx.JSON(consts.StatusInternalServerError, map[string]string{"error": "无法读取文件"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		ctx.JSON(consts.StatusInternalServerError, map[string]string{"error": "读取文件失败"})
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

func UploadZip(ctx *app.RequestContext) {
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

func BatchSyncRules(ctx *app.RequestContext) {
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
			"sensitive_type":  r.SensitiveType,
			"version":         r.Version,
			"latest_version":  resp.LatestVersion,
			"rules":           resp.Rules,
			"fingerprints":    resp.Fingerprints,
			"semantic_labels": resp.SemanticLabels,
		})
	}
	ctx.JSON(consts.StatusOK, map[string]any{"results": results})
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

func ContentScan(ctx *app.RequestContext) {
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
