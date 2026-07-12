// package config —— 读取环境变量（支持 .env）。
// 本地 PG17 配置见 backend/.env（postgres/postgres），不入库。
package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config 应用配置。
type Config struct {
	DBDSN        string
	JWTSecret    string
	AgentBaseURL string
	EmbeddingDim int
	Addr         string
	UploadDir    string
}

// Load 加载配置：先尝试读取 .env（若存在），再以环境变量覆盖。
func Load() *Config {
	_ = godotenv.Load() // 忽略缺失的 .env

	dim := 1024
	if v := os.Getenv("EMBEDDING_DIM"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			dim = n
		}
	}
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	uploadDir := os.Getenv("UPLOAD_DIR")
	if uploadDir == "" {
		uploadDir = "./data/uploads"
	}
	dbDSN := os.Getenv("DB_DSN")
	if dbDSN == "" {
		dbDSN = "postgres://postgres:postgres@localhost:5432/learning_buddy?sslmode=disable"
	}
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "dev-secret-change-me-please-32bytes+"
	}
	agentBaseURL := os.Getenv("AGENT_BASE_URL")
	if agentBaseURL == "" {
		agentBaseURL = "http://localhost:8000"
	}
	return &Config{
		DBDSN:        dbDSN,
		JWTSecret:    jwtSecret,
		AgentBaseURL: agentBaseURL,
		EmbeddingDim: dim,
		Addr:         addr,
		UploadDir:    uploadDir,
	}
}

// DSNFor 返回给 Agent 的连接串（本期 Agent 与后端共用同一库，仅读 material_chunks / 写 chunks）。
func (c *Config) DSNFor(service string) string {
	// service 仅用于可读性；本地开发共用 DB_DSN。
	_ = service
	if c.DBDSN == "" {
		return ""
	}
	return c.DBDSN
}

// String 便于日志打印（脱敏 secret）。
func (c *Config) String() string {
	return fmt.Sprintf("addr=%s agent=%s embedding_dim=%d upload_dir=%s", c.Addr, c.AgentBaseURL, c.EmbeddingDim, c.UploadDir)
}
