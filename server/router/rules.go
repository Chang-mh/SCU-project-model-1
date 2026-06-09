package router

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
	SensitiveFileID     string         `json:"sensitive_file_id"`
	FileName            string         `json:"file_name"`
	SensitiveType       string         `json:"sensitive_type"`
	RiskLevel           string         `json:"risk_level"`
	RuleVersion         int            `json:"rule_version"`
	GeneratedRulesCount int            `json:"generated_rules_count"`
	Fingerprint         map[string]any `json:"fingerprint"`
	Explanation         string         `json:"explanation"`
}

type RuleSyncResponse struct {
	LatestVersion int          `json:"latest_version"`
	FullSync      bool         `json:"full_sync"`
	Rules         []RuleResp   `json:"rules"`
	Fingerprints  []FingerResp `json:"fingerprints"`
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

func genFileID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "file_" + hex.EncodeToString(b)
}

func genRuleID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return "rule_" + hex.EncodeToString(b)
}

func UploadSample(ctx *app.RequestContext) {
	fileHeader, err := ctx.FormFile("file")
	if err != nil {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "缺少文件字段"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		ctx.JSON(consts.StatusInternalServerError, map[string]string{"error": "无法读取上传文件"})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		ctx.JSON(consts.StatusInternalServerError, map[string]string{"error": "读取文件失败"})
		return
	}

	sensitiveType := string(ctx.FormValue("sensitive_type"))
	riskLevel := string(ctx.FormValue("risk_level"))
	description := string(ctx.FormValue("description"))

	fileID := genFileID()
	fileName := fileHeader.Filename

	text, _, err := core.ExtractText(fileName, data)
	if err != nil {
		zap.L().Warn("文本解析不完整", zap.String("file", fileName), zap.Error(err))
	}
	if text == "" {
		text = fmt.Sprintf("[二进制文件: %s, 大小: %d bytes]", fileName, len(data))
	}

	sha256 := core.SHA256Hex(data)
	simhash := core.SimHashString(text)

	rules := core.GenerateRules(text, sensitiveType, riskLevel, description)
	semantic := core.AnalyzeSemantic(text, sensitiveType, riskLevel)

	var version int
	if err := dal.DB.Raw("SELECT COALESCE(MAX(version), 0) FROM rule_versions").Scan(&version).Error; err != nil {
		version = 0
	}
	newVersion := version + 1

	sample := model.SensitiveSample{
		ID:            fileID,
		FileName:      fileName,
		FileType:      filepath.Ext(fileName),
		SensitiveType: semantic.SensitiveType,
		RiskLevel:     semantic.RiskLevel,
		SHA256:        sha256,
		Explanation:   semantic.Explanation,
		ExtractedText: text,
		UploadedAt:    time.Now(),
	}
	if err := dal.DB.Create(&sample).Error; err != nil {
		zap.L().Error("保存样本失败", zap.Error(err))
		ctx.JSON(consts.StatusInternalServerError, map[string]string{"error": "保存样本失败"})
		return
	}

	for _, rule := range rules {
		r := model.GeneratedRule{
			ID:            genRuleID(),
			SampleID:      fileID,
			Version:       newVersion,
			RuleType:      rule.RuleType,
			SensitiveType: rule.SensitiveType,
			RiskLevel:     rule.RiskLevel,
			Content:       core.RuleContentJSON(rule.Content),
			CreatedAt:     time.Now(),
		}
		dal.DB.Create(&r)
	}

	fp := model.FileFingerprint{
		SampleID:   fileID,
		SHA256:     sha256,
		SimHash:    simhash,
		TextLength: len(text),
	}
	dal.DB.Create(&fp)

	ver := model.RuleVersion{
		Version:    newVersion,
		ChangeType: "upload",
		CreatedAt:  time.Now(),
	}
	dal.DB.Create(&ver)

	fingerprint := map[string]any{"sha256": sha256, "simhash": simhash}

	resp := SampleUploadResponse{
		SensitiveFileID:     fileID,
		FileName:            fileName,
		SensitiveType:       sample.SensitiveType,
		RiskLevel:           sample.RiskLevel,
		RuleVersion:         newVersion,
		GeneratedRulesCount: len(rules),
		Fingerprint:         fingerprint,
		Explanation:         sample.Explanation,
	}

	zap.L().Info("样本上传成功",
		zap.String("file_id", fileID),
		zap.String("file", fileName),
		zap.Int("rules", len(rules)),
		zap.Int("version", newVersion),
	)
	ctx.JSON(consts.StatusOK, resp)
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

	var latest model.RuleVersion
	if err := dal.DB.Order("version desc").First(&latest).Error; err != nil {
		ctx.JSON(consts.StatusOK, RuleSyncResponse{LatestVersion: 0, FullSync: false, Rules: nil, Fingerprints: nil})
		return
	}

	var rules []model.GeneratedRule
	if err := dal.DB.Where("version > ?", clientVersion).Find(&rules).Error; err != nil || len(rules) == 0 {
		ctx.JSON(consts.StatusOK, RuleSyncResponse{
			LatestVersion: latest.Version,
			FullSync:      false,
			Rules:         nil,
			Fingerprints:  nil,
		})
		return
	}

	var fingerprints []model.FileFingerprint
	dal.DB.Find(&fingerprints)

	ruleResps := make([]RuleResp, 0, len(rules))
	for _, r := range rules {
		var content map[string]any
		json.Unmarshal([]byte(r.Content), &content)
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

	resp := RuleSyncResponse{
		LatestVersion: latest.Version,
		FullSync:      clientVersion == 0,
		Rules:         ruleResps,
		Fingerprints:  fingerResps,
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
	riskLevel := string(ctx.FormValue("risk_level"))
	description := string(ctx.FormValue("description"))

	results := make([]map[string]any, 0, len(files))
	for _, fh := range files {
		f, err := fh.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			continue
		}

		fileID := genFileID()
		text, _, _ := core.ExtractText(fh.Filename, data)
		if text == "" {
			text = fmt.Sprintf("[binary: %s, %d bytes]", fh.Filename, len(data))
		}
		sha256 := core.SHA256Hex(data)
		simhash := core.SimHashString(text)
		rules := core.GenerateRules(text, sensitiveType, riskLevel, description)
		semantic := core.AnalyzeSemantic(text, sensitiveType, riskLevel)

		var version int
		dal.DB.Raw("SELECT COALESCE(MAX(version), 0) FROM rule_versions").Scan(&version)
		newVersion := version + 1

		sample := model.SensitiveSample{
			ID:            fileID,
			FileName:      fh.Filename,
			FileType:      filepath.Ext(fh.Filename),
			SensitiveType: semantic.SensitiveType,
			RiskLevel:     semantic.RiskLevel,
			SHA256:        sha256,
			Explanation:   semantic.Explanation,
			ExtractedText: text,
			UploadedAt:    time.Now(),
		}
		dal.DB.Create(&sample)
		for _, rule := range rules {
			dal.DB.Create(&model.GeneratedRule{
				ID:            genRuleID(),
				SampleID:      fileID,
				Version:       newVersion,
				RuleType:      rule.RuleType,
				SensitiveType: rule.SensitiveType,
				RiskLevel:     rule.RiskLevel,
				Content:       core.RuleContentJSON(rule.Content),
				CreatedAt:     time.Now(),
			})
		}
		dal.DB.Create(&model.FileFingerprint{SampleID: fileID, SHA256: sha256, SimHash: simhash, TextLength: len(text)})
		dal.DB.Create(&model.RuleVersion{Version: newVersion, ChangeType: "batch_upload", CreatedAt: time.Now()})

		results = append(results, map[string]any{
			"sensitive_file_id":     fileID,
			"file_name":             fh.Filename,
			"sensitive_type":        sample.SensitiveType,
			"risk_level":            sample.RiskLevel,
			"rule_version":          newVersion,
			"generated_rules_count": len(rules),
			"fingerprint":           map[string]any{"sha256": sha256, "simhash": simhash},
		})
	}

	resp := map[string]any{
		"total":   len(results),
		"results": results,
	}

	ctx.JSON(consts.StatusOK, resp)
}

func parseZip(data []byte) (map[string][]byte, error) {
	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := make(map[string][]byte)
	for _, f := range zipReader.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			continue
		}
		files[f.Name] = content
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

	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	var files map[string][]byte
	if ext == ".zip" {
		files, err = parseZip(data)
		if err != nil {
			ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "无法解析ZIP文件"})
			return
		}
	} else {
		files = map[string][]byte{fileHeader.Filename: data}
	}

	sensitiveType := string(ctx.FormValue("sensitive_type"))
	riskLevel := string(ctx.FormValue("risk_level"))
	description := string(ctx.FormValue("description"))

	results := make([]map[string]any, 0, len(files))
	for name, content := range files {
		fileID := genFileID()
		text, _, _ := core.ExtractText(name, content)
		if text == "" {
			text = fmt.Sprintf("[binary: %s, %d bytes]", name, len(content))
		}
		sha256 := core.SHA256Hex(content)
		simhash := core.SimHashString(text)
		rules := core.GenerateRules(text, sensitiveType, riskLevel, description)
		semantic := core.AnalyzeSemantic(text, sensitiveType, riskLevel)

		var version int
		dal.DB.Raw("SELECT COALESCE(MAX(version), 0) FROM rule_versions").Scan(&version)
		newVersion := version + 1

		sample := model.SensitiveSample{
			ID:            fileID,
			FileName:      name,
			FileType:      filepath.Ext(name),
			SensitiveType: semantic.SensitiveType,
			RiskLevel:     semantic.RiskLevel,
			SHA256:        sha256,
			Explanation:   semantic.Explanation,
			ExtractedText: text,
			UploadedAt:    time.Now(),
		}
		dal.DB.Create(&sample)
		for _, rule := range rules {
			dal.DB.Create(&model.GeneratedRule{
				ID:            genRuleID(),
				SampleID:      fileID,
				Version:       newVersion,
				RuleType:      rule.RuleType,
				SensitiveType: rule.SensitiveType,
				RiskLevel:     rule.RiskLevel,
				Content:       core.RuleContentJSON(rule.Content),
				CreatedAt:     time.Now(),
			})
		}
		dal.DB.Create(&model.FileFingerprint{SampleID: fileID, SHA256: sha256, SimHash: simhash, TextLength: len(text)})
		dal.DB.Create(&model.RuleVersion{Version: newVersion, ChangeType: "zip_upload", CreatedAt: time.Now()})
		results = append(results, map[string]any{
			"sensitive_file_id":     fileID,
			"file_name":             name,
			"sensitive_type":        sample.SensitiveType,
			"risk_level":            sample.RiskLevel,
			"rule_version":          newVersion,
			"generated_rules_count": len(rules),
			"fingerprint":           map[string]any{"sha256": sha256, "simhash": simhash},
		})
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
			"sensitive_type": r.SensitiveType,
			"version":        r.Version,
			"latest_version": resp.LatestVersion,
			"rules":          resp.Rules,
			"fingerprints":   resp.Fingerprints,
		})
	}
	ctx.JSON(consts.StatusOK, map[string]any{"results": results})
}

type RuleSyncQuery struct {
	SensitiveType string `json:"sensitive_type"`
	Version       int    `json:"version"`
}

func getRuleSet(clientVersion int) RuleSyncResponse {
	var latest model.RuleVersion
	if err := dal.DB.Order("version desc").First(&latest).Error; err != nil {
		return RuleSyncResponse{LatestVersion: 0}
	}
	var rules []model.GeneratedRule
	if err := dal.DB.Where("version > ?", clientVersion).Find(&rules).Error; err != nil || len(rules) == 0 {
		return RuleSyncResponse{LatestVersion: latest.Version}
	}
	var fingerprints []model.FileFingerprint
	dal.DB.Find(&fingerprints)

	ruleResps := make([]RuleResp, 0, len(rules))
	for _, r := range rules {
		var content map[string]any
		json.Unmarshal([]byte(r.Content), &content)
		ruleResps = append(ruleResps, RuleResp{
			RuleID: r.ID, RuleType: r.RuleType, SensitiveType: r.SensitiveType,
			RiskLevel: r.RiskLevel, Content: content,
		})
	}
	fingerResps := make([]FingerResp, 0, len(fingerprints))
	for _, f := range fingerprints {
		fingerResps = append(fingerResps, FingerResp{SensitiveFileID: f.SampleID, SHA256: f.SHA256, SimHash: f.SimHash})
	}
	return RuleSyncResponse{LatestVersion: latest.Version, FullSync: clientVersion == 0, Rules: ruleResps, Fingerprints: fingerResps}
}

func ContentScan(ctx *app.RequestContext) {
	var req struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(ctx.Request.Body(), &req); err != nil || req.Content == "" {
		ctx.JSON(consts.StatusBadRequest, map[string]string{"error": "缺少内容"})
		return
	}
	results := make([]map[string]any, 0)
	for _, rule := range core.BuiltinRegexRules {
		re, _ := regexp.Compile(rule.Pattern)
		matches := re.FindAllString(req.Content, -1)
		if len(matches) > 0 {
			results = append(results, map[string]any{
				"rule_name":  rule.Name,
				"pattern":    rule.Pattern,
				"risk_level": rule.RiskLevel,
				"matches":    matches,
			})
		}
	}
	ctx.JSON(consts.StatusOK, map[string]any{
		"total":   len(results),
		"results": results,
	})
}
