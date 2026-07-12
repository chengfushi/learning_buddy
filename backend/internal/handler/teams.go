package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"learning_buddy/backend/internal/middleware"
)

func (h *Handlers) listTeams(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	teams, err := h.Svc.Teams.MyTeams(c.Request.Context(), uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"teams": teams})
}

type createTeamReq struct {
	Name string `json:"name"`
}

func (h *Handlers) createTeam(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	var req createTeamReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供团队名称"})
		return
	}
	t, err := h.Svc.Teams.CreateTeacherTeam(c.Request.Context(), uid, req.Name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":        t.ID,
		"name":      t.Name,
		"type":      t.Type,
		"join_code": t.JoinCode,
		"owner_id":  t.OwnerID,
	})
}

func (h *Handlers) joinTeam(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	teamID, err := bindID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效团队"})
		return
	}
	// 团队必须存在且为 teacher team
	t, err := h.Svc.Repos.GetTeam(c.Request.Context(), teamID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "团队不存在"})
		return
	}
	if t.Type != "teacher" || t.JoinCode == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该团队不支持加入"})
		return
	}
	m, err := h.Svc.Teams.JoinByCode(c.Request.Context(), uid, *t.JoinCode)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": m.Status, "team_id": m.TeamID})
}

func (h *Handlers) approveMember(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	teamID, err := bindID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效团队"})
		return
	}
	targetID, err := bindID(c, "uid")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效成员"})
		return
	}
	if err := h.Svc.Teams.ApproveMember(c.Request.Context(), teamID, uid, targetID); err != nil {
		if errors.Is(err, middleware.ErrForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"error": "仅团队创建者可审批"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handlers) listMembers(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	teamID, err := bindID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效团队"})
		return
	}
	members, err := h.Svc.Teams.ListMembers(c.Request.Context(), teamID, uid)
	if err != nil {
		if errors.Is(err, middleware.ErrForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"error": "仅团队创建者可查看成员"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"members": members})
}

func (h *Handlers) listTeamMaterials(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	role := middleware.CtxRole(c)
	teamID, err := bindID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效团队"})
		return
	}
	items, err := h.Svc.Repos.ListTeamMaterials(c.Request.Context(), teamID, uid, role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"materials": items})
}
