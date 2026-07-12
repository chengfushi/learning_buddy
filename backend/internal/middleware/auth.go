// package middleware —— JWT 鉴权与 RBAC 校验（见 docs/system-design.md §6.2 / §9）。
package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"learning_buddy/backend/internal/service"
)

type ctxKey string

const (
	userIDKey ctxKey = "userID"
	roleKey   ctxKey = "role"
)

// ErrForbidden 越权错误。
var ErrForbidden = errors.New("forbidden")

// AuthMiddleware 校验 Bearer Token，注入 userID / role 到上下文。
func AuthMiddleware(auth *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		hdr := c.GetHeader("Authorization")
		if hdr == "" || !strings.HasPrefix(hdr, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
			return
		}
		token := strings.TrimPrefix(hdr, "Bearer ")
		claims, err := auth.VerifyToken(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "登录已失效"})
			return
		}
		c.Set(string(userIDKey), claims.UserID)
		c.Set(string(roleKey), claims.Role)
		c.Next()
	}
}

// RequireRole 限制仅指定角色可访问（RBAC 权限点）。
func RequireRole(roles ...string) gin.HandlerFunc {
	allowed := map[string]bool{}
	for _, r := range roles {
		allowed[r] = true
	}
	return func(c *gin.Context) {
		role, _ := c.Get(string(roleKey))
		rs, _ := role.(string)
		if !allowed[rs] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "无权限"})
			return
		}
		c.Next()
	}
}

// CtxUserID 从上下文取当前用户 ID。
func CtxUserID(c *gin.Context) int64 {
	v, _ := c.Get(string(userIDKey))
	id, _ := v.(int64)
	return id
}

// CtxRole 从上下文取当前用户角色。
func CtxRole(c *gin.Context) string {
	v, _ := c.Get(string(roleKey))
	role, _ := v.(string)
	return role
}
