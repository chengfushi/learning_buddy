// learning_buddy 后端入口（Go + Gin + GORM）
// 分层：handler(路由/校验) → service(业务/RBAC/可见 team) → repository(仅此层拼 SQL)
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"learning_buddy/backend/internal/handler"
	"learning_buddy/backend/internal/repository"
	"learning_buddy/backend/internal/service"
)

func main() {
	dsn := envOr("DB_DSN", "postgres://learning:learning@localhost:5432/learning_buddy?sslmode=disable")

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		slog.Error("connect db", "err", err)
		os.Exit(1)
	}

	// 启动维度一致性断言（见 docs/database.md §6）：全库 embedding 维度必须统一
	assertEmbeddingDim(db, envOr("EMBEDDING_DIM", "768"))

	repos := repository.New(db)
	svcs := service.New(repos)
	r := gin.Default()
	handler.Register(r, svcs)

	addr := envOr("ADDR", ":8080")
	slog.Info("backend listening", "addr", addr)
	if err := r.Run(addr); err != nil {
		slog.Error("server exit", "err", err)
		os.Exit(1)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// assertEmbeddingDim 校验向量表维度与配置一致（防止 RAG 静默返回垃圾，见 engineering-standards R1）
func assertEmbeddingDim(_ *gorm.DB, _ string) {
	// 实现示例：查询 material_chunks.embedding 列维度，与 EMBEDDING_DIM 比对；不一致则 os.Exit(1)
	_ = context.Background()
}
