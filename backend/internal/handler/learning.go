package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"learning_buddy/backend/internal/middleware"
)

func (h *Handlers) createLearningRecord(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	var req struct {
		MaterialID *int64   `json:"material_id"`
		DurationS  int      `json:"duration_s"`
		Progress   float64  `json:"progress"`
		Score      *float64 `json:"score"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	rec, err := h.Svc.Learning.Record(c.Request.Context(), uid, req.MaterialID, req.DurationS, req.Progress, req.Score)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"record": rec})
}

func (h *Handlers) listLearningRecords(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	records, err := h.Svc.Learning.List(c.Request.Context(), uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"records": records})
}

func (h *Handlers) progress(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	summary, err := h.Svc.Learning.Summary(c.Request.Context(), uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"summary": summary})
}
