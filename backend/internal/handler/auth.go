package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"learning_buddy/backend/internal/middleware"
	"learning_buddy/backend/internal/model"
)

type registerReq struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

func (h *Handlers) register(c *gin.Context) {
	var req registerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	u, err := h.Svc.Auth.Register(c.Request.Context(), req.Email, req.Password, req.DisplayName, req.Role)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	_, access, refresh, err := h.Svc.Auth.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "注册成功但登录失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": publicUser(u), "access_token": access, "refresh_token": refresh})
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handlers) login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	u, access, refresh, err := h.Svc.Auth.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": publicUser(u), "access_token": access, "refresh_token": refresh})
}

type refreshReq struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *Handlers) refresh(c *gin.Context) {
	var req refreshReq
	if err := c.ShouldBindJSON(&req); err != nil || req.RefreshToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 refresh_token"})
		return
	}
	access, err := h.Svc.Auth.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"access_token": access})
}

func (h *Handlers) me(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	u, err := h.Svc.Repos.GetUser(c.Request.Context(), uid)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": publicUser(u)})
}

// publicUser 脱敏输出（绝不返回 password_hash）。
func publicUser(u *model.User) gin.H {
	return gin.H{
		"id":           u.ID,
		"email":        u.Email,
		"display_name": u.DisplayName,
		"role":         u.Role,
		"subscription": u.Subscription,
		"created_at":   u.CreatedAt,
	}
}

// bindID 从路径解析 int64 ID。
func bindID(c *gin.Context, param string) (int64, error) {
	id, err := strconv.ParseInt(c.Param(param), 10, 64)
	if err != nil {
		return 0, errors.New("invalid id")
	}
	return id, nil
}
