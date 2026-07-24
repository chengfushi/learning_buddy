// learning_buddy 后端入口（Go + Gin + GORM）
// 分层：handler(路由/校验) → service(业务/RBAC/可见 team) → repository(仅此层拼 SQL)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"learning_buddy/backend/internal/config"
	"learning_buddy/backend/internal/handler"
	"learning_buddy/backend/internal/observability"
	"learning_buddy/backend/internal/repository"
	"learning_buddy/backend/internal/service"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		slog.Error("invalid backend config", "err", err)
		os.Exit(1)
	}
	slog.Info("backend starting", "config", cfg.String())

	db, err := gorm.Open(postgres.Open(cfg.DBDSN), &gorm.Config{})
	if err != nil {
		slog.Error("connect db", "err", err)
		os.Exit(1)
	}
	// 维度一致性断言（R1）：全库 embedding 维度必须统一，否则启动失败。
	if err := assertEmbeddingDim(db, cfg.EmbeddingDim); err != nil {
		slog.Error("embedding dim mismatch", "err", err)
		os.Exit(1)
	}

	repos := repository.New(db)
	svcs := service.New(repos, cfg)
	if err := svcs.Materials.RecoverParseTasks(ctx); err != nil {
		slog.Error("recover parse tasks", "err", err)
		os.Exit(1)
	}
	go svcs.Materials.RunParseDispatcher(ctx)
	r := gin.Default()
	r.Use(observability.HTTPMetrics())
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	handler.Register(r, svcs)

	addr := cfg.Addr
	slog.Info("backend listening", "addr", addr)
	server := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server exit", "err", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("server exit", "err", err)
	}
}

// assertEmbeddingDim 校验 material_chunks.embedding 列维度与配置一致（防 RAG 静默返回垃圾）。
func assertEmbeddingDim(db *gorm.DB, wantDim int) error {
	var typ string
	// pgvector 的 vector(N) 经 format_type 返回 "vector(N)"
	if err := db.Raw(
		"SELECT format_type(atttypid, atttypmod) FROM pg_attribute WHERE attrelid = 'material_chunks'::regclass AND attname = 'embedding'",
	).Scan(&typ).Error; err != nil {
		return fmt.Errorf("查询 embedding 列维度失败: %w", err)
	}
	if typ == "" {
		return fmt.Errorf("material_chunks.embedding 列不存在")
	}
	// 解析 "vector(768)" 中的数字
	start := strings.Index(typ, "(")
	end := strings.Index(typ, ")")
	if start < 0 || end < 0 {
		return fmt.Errorf("无法解析 embedding 列类型: %s", typ)
	}
	dim, err := strconv.Atoi(typ[start+1 : end])
	if err != nil {
		return fmt.Errorf("无法解析 embedding 维度: %s", typ)
	}
	if dim != wantDim {
		return fmt.Errorf("embedding 维度不一致：库表为 %d，配置为 %d（全库必须统一，见 R1）", dim, wantDim)
	}
	slog.Info("embedding dim ok", "dim", dim)
	return nil
}
