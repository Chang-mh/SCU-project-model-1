package main

import (
	"context"
	"os"
	"path/filepath"

	"scu-project-model-1/server/core"
	"scu-project-model-1/server/dal"
	"scu-project-model-1/server/router"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func wrap(handler func(*app.RequestContext)) app.HandlerFunc {
	return func(_ context.Context, c *app.RequestContext) {
		handler(c)
	}
}

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

	h := server.Default(server.WithHostPorts(addr))

	h.POST("/api/server/samples", wrap(router.UploadSample))
	h.POST("/api/server/samples/batch", wrap(router.UploadSamplesBatch))
	h.POST("/api/server/samples/zip", wrap(router.UploadZip))
	h.GET("/api/server/samples", wrap(router.ListSamples))
	h.GET("/api/server/sensitive-files", wrap(router.GetSensitiveFilesList))
	h.GET("/api/server/sensitive-files/:file_hash", wrap(router.GetSensitiveFileInfo))
	h.POST("/api/server/sensitive-files/query", wrap(router.QuerySensitiveFile))
	h.POST("/api/server/regex-test", wrap(router.RegexTest))
	h.POST("/api/server/fingerprint", wrap(router.FingerprintCompute))
	h.POST("/api/server/content-scan", wrap(router.ContentScan))

	h.GET("/api/client/rules", wrap(router.SyncRules))
	h.POST("/api/client/rules/batch", wrap(router.BatchSyncRules))

	zap.L().Info("敏感文件识别服务启动", zap.String("addr", addr))
	h.Spin()
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
