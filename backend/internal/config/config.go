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
	DBDSN                string
	JWTSecret            string
	AgentBaseURL         string
	AgentSharedSecret    string
	RedisAddr            string
	ChatRateLimitPerMin  int
	ChatDailyTokenLimit  int
	ParseAlertWebhookURL string
	EmbeddingDim         int
	Addr                 string
	UploadDir            string
	MinIOEndpoint        string
	MinIOPublicEndpoint  string
	MinIOAccessKey       string
	MinIOSecretKey       string
	MinIOSecure          bool
	MinIOPublicSecure    bool
	MinIORegion          string
	MinIOSourceBucket    string
	MinIODerivedBucket   string
	AssetURLTTLSeconds   int
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
	minIOEndpoint := envOr("MINIO_ENDPOINT", "localhost:9000")
	minIOSecure := envBool("MINIO_SECURE", false)
	minIOPublicSecure := minIOSecure
	if _, configured := os.LookupEnv("MINIO_PUBLIC_SECURE"); configured {
		minIOPublicSecure = envBool("MINIO_PUBLIC_SECURE", false)
	}
	return &Config{
		DBDSN:                dbDSN,
		JWTSecret:            jwtSecret,
		AgentBaseURL:         agentBaseURL,
		AgentSharedSecret:    os.Getenv("AGENT_SHARED_SECRET"),
		RedisAddr:            envOr("REDIS_ADDR", ""),
		ChatRateLimitPerMin:  envInt("CHAT_RATE_LIMIT_PER_MIN", 20),
		ChatDailyTokenLimit:  envInt("CHAT_DAILY_TOKEN_LIMIT", 100000),
		ParseAlertWebhookURL: os.Getenv("PARSE_ALERT_WEBHOOK_URL"),
		EmbeddingDim:         dim,
		Addr:                 addr,
		UploadDir:            uploadDir,
		MinIOEndpoint:        minIOEndpoint,
		MinIOPublicEndpoint:  envOr("MINIO_PUBLIC_ENDPOINT", minIOEndpoint),
		MinIOAccessKey:       envOr("MINIO_ACCESS_KEY", "minioadmin"),
		MinIOSecretKey:       envOr("MINIO_SECRET_KEY", "minioadmin"),
		MinIOSecure:          minIOSecure,
		MinIOPublicSecure:    minIOPublicSecure,
		MinIORegion:          envOr("MINIO_REGION", "us-east-1"),
		MinIOSourceBucket:    envOr("MINIO_SOURCE_BUCKET", "materials-source"),
		MinIODerivedBucket:   envOr("MINIO_DERIVED_BUCKET", "materials-derived"),
		AssetURLTTLSeconds:   envInt("ASSET_URL_TTL_SECONDS", 600),
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if value := os.Getenv(key); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

// Validate 校验生产安全所需的必填配置。
func (c *Config) Validate() error {
	if c.AgentSharedSecret == "" {
		return fmt.Errorf("AGENT_SHARED_SECRET is required")
	}
	return nil
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
