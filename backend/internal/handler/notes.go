package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"learning_buddy/backend/internal/middleware"
	"learning_buddy/backend/internal/model"
)

func (h *Handlers) listNotes(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	materialID, err := bindID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效资料"})
		return
	}
	notes, err := h.Svc.Repos.ListNotesForVisibleMaterial(c.Request.Context(), uid, materialID)
	if err != nil {
		writeMaterialReadErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"notes": notes})
}

type noteReq struct {
	Content string  `json:"content"`
	Quote   *string `json:"quote"`
}

func (h *Handlers) createNote(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	materialID, err := bindID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效资料"})
		return
	}
	var req noteReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "笔记内容必填"})
		return
	}
	n := &model.MaterialNote{
		UserID:     uid,
		MaterialID: materialID,
		Content:    req.Content,
		Quote:      req.Quote,
	}
	if err := h.Svc.Repos.CreateNoteForVisibleMaterial(c.Request.Context(), n); err != nil {
		writeMaterialReadErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"note": n})
}

type noteUpdateReq struct {
	Content string `json:"content"`
}

func (h *Handlers) updateNote(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	id, err := bindID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效笔记"})
		return
	}
	var req noteUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "笔记内容必填"})
		return
	}
	if err := h.Svc.Repos.UpdateNote(c.Request.Context(), id, uid, req.Content); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handlers) deleteNote(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	id, err := bindID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效笔记"})
		return
	}
	if err := h.Svc.Repos.DeleteNote(c.Request.Context(), id, uid); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
