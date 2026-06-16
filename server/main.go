package main

import (
	"context"
	"crypto/subtle"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"scu-project-model-1/server/core"
	"scu-project-model-1/server/dal"
	"scu-project-model-1/server/router"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()
	zap.ReplaceGlobals(logger)

	// 加载 .env 文件 (从当前目录向上查找)
	if envFile := findEnvFile(); envFile != "" {
		if err := godotenv.Load(envFile); err != nil {
			zap.L().Warn("加载 .env 文件失败", zap.String("path", envFile), zap.Error(err))
		} else {
			zap.L().Info("已加载 .env 文件", zap.String("path", envFile))
		}
	}

	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		dsn = "root:password@tcp(127.0.0.1:3306)/sensitive_agent?charset=utf8mb4&parseTime=True&loc=Local"
	}
	if err := dal.InitDB(dsn); err != nil {
		zap.L().Fatal("数据库连接失败, 请设置 MYSQL_DSN", zap.Error(err))
	}

	addr := os.Getenv("SERVER_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	// 检查火山方舟大模型配置状态
	if core.IsLLMReady() {
		zap.L().Info("火山方舟大模型已就绪, 语义识别将使用大模型")
	} else {
		zap.L().Warn("火山方舟未配置, 语义识别将使用规则推理降级方案")
	}

	maxRequestBodySize := maxRequestBodySizeBytes()
	h := server.Default(
		server.WithHostPorts(addr),
		server.WithMaxRequestBodySize(maxRequestBodySize),
	)
	h.Use(apiTokenAuthMiddleware(os.Getenv("SERVER_API_TOKEN")))

	h.POST("/api/server/samples", router.UploadSample)
	h.POST("/api/server/samples/batch", router.UploadSamplesBatch)
	h.POST("/api/server/samples/zip", router.UploadZip)
	h.GET("/api/server/samples", router.ListSamples)
	h.GET("/api/server/sensitive-files", router.GetSensitiveFilesList)
	h.GET("/api/server/sensitive-files/:file_hash", router.GetSensitiveFileInfo)
	h.POST("/api/server/sensitive-files/query", router.QuerySensitiveFile)
	h.POST("/api/server/regex-test", router.RegexTest)
	h.POST("/api/server/fingerprint", router.FingerprintCompute)
	h.POST("/api/server/content-scan", router.ContentScan)
	h.POST("/api/server/semantic-search", router.SemanticSearch)
	h.PATCH("/api/server/rules/:rule_id", router.UpdateRule)
	h.DELETE("/api/server/rules/:rule_id", router.DeleteRule)

	h.GET("/api/client/rules", router.SyncRules)
	h.POST("/api/client/rules/batch", router.BatchSyncRules)
	h.POST("/api/client/scan-results", router.ReportScanResults)

	h.GET("/api/server/scan-results", router.ListScanResults)

	go waitForShutdown(h, logger)

	zap.L().Info("敏感文件识别服务启动", zap.String("addr", addr), zap.Int("max_request_body_size", maxRequestBodySize))
	h.Spin()
}

func apiTokenAuthMiddleware(token string) app.HandlerFunc {
	token = strings.TrimSpace(token)
	if token == "" || token == "change-me" {
		if token == "change-me" {
			zap.L().Warn("SERVER_API_TOKEN 使用默认占位值，API Token 鉴权未启用")
		} else {
			zap.L().Warn("SERVER_API_TOKEN 未配置，API Token 鉴权未启用")
		}
		return func(c context.Context, ctx *app.RequestContext) {
			ctx.Next(c)
		}
	}

	expected := "Bearer " + token
	return func(c context.Context, ctx *app.RequestContext) {
		authorization := strings.TrimSpace(ctx.Request.Header.Get(consts.HeaderAuthorization))
		if subtle.ConstantTimeCompare([]byte(authorization), []byte(expected)) != 1 {
			ctx.AbortWithStatusJSON(consts.StatusUnauthorized, map[string]string{"error": "未授权，请提供有效的 Authorization: Bearer <token>"})
			return
		}
		ctx.Next(c)
	}
}

func maxRequestBodySizeBytes() int {
	const defaultMB = 220
	mb := defaultMB
	if value := os.Getenv("MAX_REQUEST_BODY_SIZE_MB"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			zap.L().Warn("MAX_REQUEST_BODY_SIZE_MB 无效，使用默认值", zap.String("value", value), zap.Int("default_mb", defaultMB))
		} else {
			mb = parsed
		}
	}
	if mb < 1 {
		mb = 1
	}
	if mb > 512 {
		zap.L().Warn("MAX_REQUEST_BODY_SIZE_MB 超过上限，已限制为 512MB", zap.Int("requested_mb", mb))
		mb = 512
	}
	return mb * 1024 * 1024
}

func waitForShutdown(h *server.Hertz, logger *zap.Logger) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	zap.L().Info("收到退出信号，正在优雅关闭服务")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		zap.L().Error("HTTP 服务关闭失败", zap.Error(err))
	}
	if dal.DB != nil {
		if sqlDB, err := dal.DB.DB(); err == nil {
			if err := sqlDB.Close(); err != nil {
				zap.L().Warn("数据库连接关闭失败", zap.Error(err))
			}
		}
	}
	_ = logger.Sync()
}

// findEnvFile 从当前目录向上查找 .env 文件
func findEnvFile() string {
	dir, _ := os.Getwd()
	for i := 0; i < 5; i++ {
		p := filepath.Join(dir, ".env")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
