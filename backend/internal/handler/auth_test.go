package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"learning_buddy/backend/internal/config"
	"learning_buddy/backend/internal/repository"
	"learning_buddy/backend/internal/service"
)

const handlerJWTSecret = "handler-test-secret"

func TestAuthHTTPContract(t *testing.T) {
	router := newTestRouter(t)
	suffix := uuid.NewString()[:8]
	email := "handler_" + suffix + "@test.dev"
	password := "password123"

	t.Run("protected route rejects anonymous request", func(t *testing.T) {
		resp := performRequest(t, router, http.MethodGet, "/api/me", "", "")
		assert.Equal(t, http.StatusUnauthorized, resp.Code)
		assertJSONField(t, resp.Body.Bytes(), "error", "未登录")
	})

	t.Run("register rejects malformed JSON", func(t *testing.T) {
		resp := performRequest(t, router, http.MethodPost, "/api/auth/register", "{", "")
		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assertJSONField(t, resp.Body.Bytes(), "error", "请求格式错误")
	})

	t.Run("register rejects reserved role", func(t *testing.T) {
		body := `{"email":"reserved@example.test","password":"password123","display_name":"越权注册","role":"super_admin"}`
		resp := performRequest(t, router, http.MethodPost, "/api/auth/register", body, "")
		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assertJSONField(t, resp.Body.Bytes(), "error", "不允许注册该角色")
	})

	registerBody := `{"email":"` + email + `","password":"` + password + `","display_name":"接口测试","role":"student"}`
	registerResp := performRequest(t, router, http.MethodPost, "/api/auth/register", registerBody, "")
	require.Equal(t, http.StatusOK, registerResp.Code)
	registerPayload := decodeJSONObject(t, registerResp.Body.Bytes())
	registerAccess := requireStringField(t, registerPayload, "access_token")
	registerRefresh := requireStringField(t, registerPayload, "refresh_token")
	assertPublicUserPayload(t, registerResp.Body.Bytes(), registerPayload, email)

	t.Run("register rejects duplicate account", func(t *testing.T) {
		resp := performRequest(t, router, http.MethodPost, "/api/auth/register", registerBody, "")
		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assert.NotEmpty(t, requireStringField(t, decodeJSONObject(t, resp.Body.Bytes()), "error"))
	})

	t.Run("login rejects malformed JSON", func(t *testing.T) {
		resp := performRequest(t, router, http.MethodPost, "/api/auth/login", "{", "")
		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assertJSONField(t, resp.Body.Bytes(), "error", "请求格式错误")
	})

	t.Run("login rejects wrong password", func(t *testing.T) {
		body := `{"email":"` + email + `","password":"incorrect-password"}`
		resp := performRequest(t, router, http.MethodPost, "/api/auth/login", body, "")
		assert.Equal(t, http.StatusUnauthorized, resp.Code)
		assertJSONField(t, resp.Body.Bytes(), "error", "账号或密码错误")
	})

	loginBody := `{"email":"` + email + `","password":"` + password + `"}`
	loginResp := performRequest(t, router, http.MethodPost, "/api/auth/login", loginBody, "")
	require.Equal(t, http.StatusOK, loginResp.Code)
	loginPayload := decodeJSONObject(t, loginResp.Body.Bytes())
	loginAccess := requireStringField(t, loginPayload, "access_token")
	assert.NotEmpty(t, requireStringField(t, loginPayload, "refresh_token"))
	assertPublicUserPayload(t, loginResp.Body.Bytes(), loginPayload, email)

	t.Run("refresh rejects missing token", func(t *testing.T) {
		resp := performRequest(t, router, http.MethodPost, "/api/auth/refresh", `{}`, "")
		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assertJSONField(t, resp.Body.Bytes(), "error", "缺少 refresh_token")
	})

	t.Run("refresh rejects invalid token", func(t *testing.T) {
		resp := performRequest(t, router, http.MethodPost, "/api/auth/refresh", `{"refresh_token":"invalid"}`, "")
		assert.Equal(t, http.StatusUnauthorized, resp.Code)
		assertJSONField(t, resp.Body.Bytes(), "error", "无效 refresh token")
	})

	t.Run("refresh returns a verifiable access token", func(t *testing.T) {
		body := `{"refresh_token":"` + registerRefresh + `"}`
		resp := performRequest(t, router, http.MethodPost, "/api/auth/refresh", body, "")
		require.Equal(t, http.StatusOK, resp.Code)
		payload := decodeJSONObject(t, resp.Body.Bytes())
		assert.NotEmpty(t, requireStringField(t, payload, "access_token"))
	})

	t.Run("me returns a redacted public user", func(t *testing.T) {
		resp := performRequest(t, router, http.MethodGet, "/api/me", "", loginAccess)
		require.Equal(t, http.StatusOK, resp.Code)
		assertPublicUserPayload(t, resp.Body.Bytes(), decodeJSONObject(t, resp.Body.Bytes()), email)
	})

	t.Run("me rejects a token signed by another key", func(t *testing.T) {
		token := signHandlerToken(t, "wrong-secret", 999_999, "student")
		resp := performRequest(t, router, http.MethodGet, "/api/me", "", token)
		assert.Equal(t, http.StatusUnauthorized, resp.Code)
		assertJSONField(t, resp.Body.Bytes(), "error", "登录已失效")
	})

	t.Run("me returns not found for a valid but unknown user", func(t *testing.T) {
		token := signHandlerToken(t, handlerJWTSecret, 9_000_000_000, "student")
		resp := performRequest(t, router, http.MethodGet, "/api/me", "", token)
		assert.Equal(t, http.StatusNotFound, resp.Code)
		assertJSONField(t, resp.Body.Bytes(), "error", "用户不存在")
	})

	assert.NotEmpty(t, registerAccess)
}

func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	router, _ := newTestRouterWithServices(t)
	return router
}

func newTestRouterWithServices(t *testing.T) (*gin.Engine, *service.Services) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/learning_buddy?sslmode=disable"
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	tx := db.Begin()
	require.NoError(t, tx.Error)
	t.Cleanup(func() {
		require.NoError(t, tx.Rollback().Error)
	})

	cfg := &config.Config{
		DBDSN:             dsn,
		JWTSecret:         handlerJWTSecret,
		AgentBaseURL:      "http://127.0.0.1:1",
		AgentSharedSecret: "handler-agent-secret",
		EmbeddingDim:      1024,
	}
	svcs := service.New(repository.New(tx), cfg)
	router := gin.New()
	Register(router, svcs)
	return router, svcs
}

func performRequest(t *testing.T, router http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}

func decodeJSONObject(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	return payload
}

func requireStringField(t *testing.T, payload map[string]any, key string) string {
	t.Helper()
	value, ok := payload[key].(string)
	require.True(t, ok, "字段 %s 必须是字符串", key)
	return value
}

func assertJSONField(t *testing.T, body []byte, key, want string) {
	t.Helper()
	assert.Equal(t, want, requireStringField(t, decodeJSONObject(t, body), key))
}

func assertPublicUserPayload(t *testing.T, raw []byte, payload map[string]any, wantEmail string) {
	t.Helper()
	user, ok := payload["user"].(map[string]any)
	require.True(t, ok, "响应必须包含 user 对象")
	assert.Equal(t, wantEmail, user["email"])
	assert.Equal(t, "student", user["role"])
	assert.Equal(t, "free", user["subscription"])
	assert.NotEmpty(t, user["id"])
	assert.NotEmpty(t, user["created_at"])
	assert.NotContains(t, user, "password_hash")
	assert.NotContains(t, strings.ToLower(string(raw)), "password123")
}

func signHandlerToken(t *testing.T, secret string, userID int64, role string) string {
	t.Helper()
	claims := service.Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	require.NoError(t, err)
	return token
}
