package main

import (
	"context"
	"os"

	"scu-project-model-1/server/dal"
	"scu-project-model-1/server/router"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
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

	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		dsn = "root:password@tcp(127.0.0.1:3306)/sensitive_agent?charset=utf8mb4&parseTime=True&loc=Local"
	}
	if err := dal.InitDB(dsn); err != nil {
		zap.L().Fatal("数据库连接失败，请设置 MYSQL_DSN", zap.Error(err))
	}

	addr := os.Getenv("SERVER_ADDR")
	if addr == "" {
		addr = ":8080"
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
