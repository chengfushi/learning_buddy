package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"learning_buddy/backend/internal/middleware"
	"learning_buddy/backend/internal/model"
	"learning_buddy/backend/internal/repository"
	"learning_buddy/backend/internal/service"
	objectstorage "learning_buddy/backend/internal/storage"
)

func (h *Handlers) listMaterials(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	visible, err := h.Svc.Teams.VisibleTeamIDs(c.Request.Context(), uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var teamID *int64
	if v := c.Query("team_id"); v != "" {
		if id, err := bindIDStr(v); err == nil {
			teamID = &id
		}
	}
	q := c.Query("q")
	limit := 100
	if v := c.Query("limit"); v != "" {
		if n, err := bindIDStr(v); err == nil && n > 0 {
			limit = int(n)
		}
	}
	items, err := h.Svc.Repos.ListVisibleMaterials(c.Request.Context(), uid, visible, teamID, q, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"materials": items})
}

func (h *Handlers) createMaterial(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	role := middleware.CtxRole(c)

	ct := c.ContentType()
	if strings.HasPrefix(ct, "multipart/") {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, objectstorage.MaxUploadBytes+(1<<20))
		teamID, err := bindID(c, "team_id")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 team_id"})
			return
		}
		in := h.buildCreateInput(teamID, uid, c)
		if in.Title == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请提供资料标题"})
			return
		}
		file, fileErr := c.FormFile("file")
		var m *model.Material
		if fileErr == nil && file != nil {
			if file.Size > objectstorage.MaxUploadBytes {
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "文件不能超过 50 MiB"})
				return
			}
			source, openErr := file.Open()
			if openErr != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "无法读取上传文件"})
				return
			}
			defer func() { _ = source.Close() }()
			m, err = h.Svc.Materials.CreateWithFile(
				c.Request.Context(), uid, role, in, file.Filename,
				file.Header.Get("Content-Type"), source,
			)
		} else {
			m, err = h.Svc.Materials.Create(c.Request.Context(), uid, role, in)
		}
		if err != nil {
			writeMaterialErr(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"material": m})
		return
	}

	// JSON 请求
	var j struct {
		TeamID   int64    `json:"team_id"`
		Title    string   `json:"title"`
		Subject  *string  `json:"subject"`
		Chapter  *string  `json:"chapter"`
		Tags     []string `json:"tags"`
		Content  *string  `json:"content"`
		FileType *string  `json:"file_type"`
	}
	if err := c.ShouldBindJSON(&j); err != nil || j.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供 team_id 与 title"})
		return
	}
	in := service.CreateInput{
		TeamID:   j.TeamID,
		Title:    j.Title,
		Subject:  j.Subject,
		Chapter:  j.Chapter,
		Tags:     model.StringArray(j.Tags),
		Content:  j.Content,
		FileType: j.FileType,
		OwnerID:  uid,
	}
	m, err := h.Svc.Materials.Create(c.Request.Context(), uid, role, in)
	if err != nil {
		writeMaterialErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"material": m})
}

func writeMaterialErr(c *gin.Context, err error) {
	if errors.Is(err, middleware.ErrForbidden) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权向该团队上传资料"})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}

func (h *Handlers) getMaterial(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	id, err := bindID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效资料"})
		return
	}
	m, err := h.Svc.Repos.GetVisibleMaterial(c.Request.Context(), uid, id)
	if err != nil {
		writeMaterialReadErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"material": m})
}

func (h *Handlers) getMaterialSourceURL(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	id, err := bindID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效资料"})
		return
	}
	material, err := h.Svc.Repos.GetVisibleMaterial(c.Request.Context(), uid, id)
	if err != nil || material.StorageKey == nil || *material.StorageKey == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "原文件不存在"})
		return
	}
	if h.Svc.Objects == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储不可用"})
		return
	}
	value, err := h.Svc.Objects.PresignSource(c.Request.Context(), *material.StorageKey)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "生成下载地址失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": value, "expires_in": h.Svc.Cfg.AssetURLTTLSeconds})
}

func (h *Handlers) listMaterialAssets(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	id, err := bindID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效资料"})
		return
	}
	assets, err := h.Svc.Repos.ListVisibleMaterialAssets(c.Request.Context(), uid, id)
	if err != nil {
		writeMaterialReadErr(c, err)
		return
	}
	type assetView struct {
		ID         int64   `json:"id"`
		PageNumber *int    `json:"page_number"`
		Caption    *string `json:"caption"`
		OCRText    *string `json:"ocr_text"`
		URL        string  `json:"url"`
	}
	items := make([]assetView, 0, len(assets))
	for _, asset := range assets {
		url := ""
		if h.Svc.Objects != nil {
			url, _ = h.Svc.Objects.PresignDerived(c.Request.Context(), asset.StorageKey)
		}
		items = append(items, assetView{
			ID: asset.ID, PageNumber: asset.PageNumber, Caption: asset.Caption,
			OCRText: asset.OCRText, URL: url,
		})
	}
	c.JSON(http.StatusOK, gin.H{"assets": items})
}

func (h *Handlers) getMaterialProcessing(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	id, err := bindID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效资料"})
		return
	}
	run, err := h.Svc.Repos.GetVisibleProcessingRun(c.Request.Context(), uid, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusOK, gin.H{"processing": nil})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取解析进度失败"})
		return
	}
	progress := make(map[string]any)
	_ = json.Unmarshal(run.Progress, &progress)
	c.JSON(http.StatusOK, gin.H{"processing": gin.H{
		"ID": run.ID, "MaterialID": run.MaterialID,
		"ParseGeneration": run.ParseGeneration, "IndexVersion": run.IndexVersion,
		"Stage": run.Stage, "Status": run.Status, "Progress": progress,
	}})
}

func writeMaterialReadErr(c *gin.Context, err error) {
	if errors.Is(err, repository.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "资料不存在"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "读取资料失败"})
}

func (h *Handlers) updateMaterial(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	role := middleware.CtxRole(c)
	id, err := bindID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效资料"})
		return
	}
	var body struct {
		Title   *string `json:"title"`
		Content *string `json:"content"`
		Shared  *bool   `json:"shared"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	m, err := h.Svc.Materials.Update(c.Request.Context(), uid, role, id, body.Title, body.Content, body.Shared)
	if err != nil {
		if errors.Is(err, middleware.ErrForbidden) || errors.Is(err, service.ErrForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权修改该资料"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"material": m})
}

func (h *Handlers) deleteMaterial(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	role := middleware.CtxRole(c)
	id, err := bindID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效资料"})
		return
	}
	if err := h.Svc.Materials.Delete(c.Request.Context(), uid, role, id); err != nil {
		if errors.Is(err, middleware.ErrForbidden) || errors.Is(err, service.ErrForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权删除该资料"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handlers) retryMaterialParse(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	role := middleware.CtxRole(c)
	id, err := bindID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效资料"})
		return
	}
	m, err := h.Svc.Materials.RetryParse(c.Request.Context(), uid, role, id)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": "无权重试该资料"})
		case errors.Is(err, service.ErrMaterialParseNotFailed):
			c.JSON(http.StatusConflict, gin.H{"error": "仅解析失败的资料可重试"})
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"material": m})
}

// buildCreateInput 组装资料创建入参（兼容 JSON / 表单 / 文件上传）。
func (h *Handlers) buildCreateInput(teamID, uid int64, c *gin.Context) service.CreateInput {
	in := service.CreateInput{TeamID: teamID, OwnerID: uid}
	in.Title = strings.TrimSpace(c.PostForm("title"))
	if v := c.PostForm("subject"); v != "" {
		in.Subject = &v
	}
	if v := c.PostForm("chapter"); v != "" {
		in.Chapter = &v
	}
	if v := c.PostForm("tags"); v != "" {
		in.Tags = model.StringArray(strings.Split(v, ","))
	}
	if v := c.PostForm("content"); v != "" {
		in.Content = &v
	}
	if v := c.PostForm("file_type"); v != "" {
		in.FileType = &v
	}
	return in
}

func bindIDStr(s string) (int64, error) {
	var id int64
	_, err := fmt.Sscanf(s, "%d", &id)
	return id, err
}
