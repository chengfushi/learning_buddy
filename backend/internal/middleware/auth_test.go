package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"learning_buddy/backend/internal/config"
	"learning_buddy/backend/internal/service"
)

const testJWTSecret = "middleware-test-secret"

func TestAuthMiddlewareRejectsMissingAndInvalidTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := service.NewAuthService(nil, &config.Config{JWTSecret: testJWTSecret})

	tests := []struct {
		name          string
		authorization string
		wantError     string
	}{
		{name: "missing header", wantError: "未登录"},
		{name: "wrong scheme", authorization: "Basic abc", wantError: "未登录"},
		{name: "empty bearer token", authorization: "Bearer ", wantError: "登录已失效"},
		{name: "malformed token", authorization: "Bearer not-a-jwt", wantError: "登录已失效"},
		{
			name:          "wrong signature",
			authorization: "Bearer " + signTestToken(t, "different-secret", 42, "teacher"),
			wantError:     "登录已失效",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			router := gin.New()
			router.GET("/protected", AuthMiddleware(auth), func(c *gin.Context) {
				called = true
				c.Status(http.StatusNoContent)
			})

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)

			assert.Equal(t, http.StatusUnauthorized, resp.Code)
			assert.False(t, called, "被拒绝的请求不得执行下游 handler")
			assertJSONError(t, resp.Body.Bytes(), tt.wantError)
		})
	}
}

func TestAuthMiddlewareInjectsClaims(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := service.NewAuthService(nil, &config.Config{JWTSecret: testJWTSecret})
	router := gin.New()
	router.GET("/protected", AuthMiddleware(auth), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"user_id": CtxUserID(c),
			"role":    CtxRole(c),
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, testJWTSecret, 42, "teacher"))
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	var body struct {
		UserID int64  `json:"user_id"`
		Role   string `json:"role"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, int64(42), body.UserID)
	assert.Equal(t, "teacher", body.Role)
}

func TestRequireRoleAllowsOnlyConfiguredRoles(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		role       any
		setRole    bool
		wantStatus int
		wantCalled bool
	}{
		{name: "allowed teacher", role: "teacher", setRole: true, wantStatus: http.StatusNoContent, wantCalled: true},
		{name: "allowed administrator", role: "super_admin", setRole: true, wantStatus: http.StatusNoContent, wantCalled: true},
		{name: "denied student", role: "student", setRole: true, wantStatus: http.StatusForbidden},
		{name: "missing role", wantStatus: http.StatusForbidden},
		{name: "wrong context type", role: int64(1), setRole: true, wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			router := gin.New()
			router.GET("/role", func(c *gin.Context) {
				if tt.setRole {
					c.Set(string(roleKey), tt.role)
				}
				c.Next()
			}, RequireRole("teacher", "super_admin"), func(c *gin.Context) {
				called = true
				c.Status(http.StatusNoContent)
			})

			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/role", nil))

			assert.Equal(t, tt.wantStatus, resp.Code)
			assert.Equal(t, tt.wantCalled, called)
			if !tt.wantCalled {
				assertJSONError(t, resp.Body.Bytes(), "无权限")
			}
		})
	}
}

func TestAuthAndRoleMiddlewareComposition(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := service.NewAuthService(nil, &config.Config{JWTSecret: testJWTSecret})

	tests := []struct {
		name       string
		role       string
		wantStatus int
		wantCalled bool
	}{
		{name: "teacher accepted", role: "teacher", wantStatus: http.StatusNoContent, wantCalled: true},
		{name: "student rejected", role: "student", wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			router := gin.New()
			router.GET("/teacher", AuthMiddleware(auth), RequireRole("teacher"), func(c *gin.Context) {
				called = true
				c.Status(http.StatusNoContent)
			})

			req := httptest.NewRequest(http.MethodGet, "/teacher", nil)
			req.Header.Set("Authorization", "Bearer "+signTestToken(t, testJWTSecret, 7, tt.role))
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)

			assert.Equal(t, tt.wantStatus, resp.Code)
			assert.Equal(t, tt.wantCalled, called)
		})
	}
}

func TestContextAccessorsReturnZeroValuesForUnexpectedData(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	assert.Zero(t, CtxUserID(c))
	assert.Empty(t, CtxRole(c))

	c.Set(string(userIDKey), "not-an-int64")
	c.Set(string(roleKey), int64(1))
	assert.Zero(t, CtxUserID(c))
	assert.Empty(t, CtxRole(c))
}

func signTestToken(t *testing.T, secret string, userID int64, role string) string {
	t.Helper()
	claims := service.Claims{
		UserID:    userID,
		Role:      role,
		TokenType: "access",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "42",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	require.NoError(t, err)
	return token
}

func assertJSONError(t *testing.T, body []byte, want string) {
	t.Helper()
	var payload struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(body, &payload))
	assert.Equal(t, want, payload.Error)
}
