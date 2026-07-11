// package handler —— 路由与请求校验。不写业务，不拼权限 SQL。
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"learning_buddy/backend/internal/service"
)

// Handlers 聚合所有 handler。
type Handlers struct {
	Svc *service.Services
}

// Register 注册路由。
func Register(r *gin.Engine, svc *service.Services) {
	h := &Handlers{Svc: svc}
	api := r.Group("/api")
	api.GET("/materials", h.listMaterials)
}

// listMaterials 示例：从上下文取 userID → 算可见 team 集合 → repository 过滤。
// 权限隔离完全发生在 repository 层，handler 只负责编排。
func (h *Handlers) listMaterials(c *gin.Context) {
	userID, _ := c.Get("userID") // 由 JWT 中间件注入
	teamIDs, err := h.Svc.Teams.ComputeVisibleTeamIDs(c.Request.Context(), userID.(int64))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items, err := h.Svc.Repos.ListVisible(teamIDs, c.Query("q"), 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
