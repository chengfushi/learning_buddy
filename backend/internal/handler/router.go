// package handler —— 路由注册与请求校验。不写业务与权限 SQL（权限在 repository/service 层）。
package handler

import (
	"github.com/gin-gonic/gin"

	"learning_buddy/backend/internal/middleware"
	"learning_buddy/backend/internal/service"
)

// Handlers 聚合所有 handler。
type Handlers struct {
	Svc  *service.Services
	Auth *service.AuthService
}

// Register 注册全部路由。
func Register(r *gin.Engine, svc *service.Services) {
	h := &Handlers{Svc: svc, Auth: svc.Auth}
	api := r.Group("/api")

	// 公开接口
	api.POST("/auth/register", h.register)
	api.POST("/auth/login", h.login)
	api.POST("/auth/refresh", h.refresh)

	// 鉴权接口
	auth := api.Group("")
	auth.Use(middleware.AuthMiddleware(svc.Auth))
	{
		auth.GET("/me", h.me)

		// 团队 / 知识库（F2）
		auth.GET("/teams", h.listTeams)
		auth.POST("/teams", middleware.RequireRole("teacher"), h.createTeam)
		auth.POST("/teams/:id/join", middleware.RequireRole("student"), h.joinTeam)
		auth.POST("/teams/:id/members/:uid/approve", middleware.RequireRole("teacher"), h.approveMember)
		auth.GET("/teams/:id/members", middleware.RequireRole("teacher"), h.listMembers)
		auth.GET("/teams/:id/materials", h.listTeamMaterials)

		// 资料（F2.2/2.4）
		auth.GET("/materials", h.listMaterials)
		auth.POST("/materials", h.createMaterial)
		auth.GET("/materials/:id", h.getMaterial)
		auth.PUT("/materials/:id", h.updateMaterial)
		auth.DELETE("/materials/:id", h.deleteMaterial)

		// 笔记（F3）
		auth.GET("/materials/:id/notes", h.listNotes)
		auth.POST("/materials/:id/notes", h.createNote)
		auth.PUT("/notes/:id", h.updateNote)
		auth.DELETE("/notes/:id", h.deleteNote)

		// 学习记录（F6）
		auth.POST("/learning/records", h.createLearningRecord)
		auth.GET("/learning/records", h.listLearningRecords)
		auth.GET("/learning/progress", h.progress)

		// 对话 / Agent（F4/F5/F7/F8）
		auth.GET("/agent/sessions", h.listSessions)
		auth.GET("/agent/sessions/:id", h.getSession)
		auth.POST("/agent/chat", h.chat)                  // SSE 流式
		auth.POST("/agent/plan", h.createPlan)            // F7
		auth.POST("/agent/quiz", h.createQuiz)            // F8 生成
		auth.POST("/agent/quiz/:id/answer", h.answerQuiz) // F8 提交批改
	}
}
