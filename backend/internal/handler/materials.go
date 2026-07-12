package handler

import (
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"learning_buddy/backend/internal/middleware"
	"learning_buddy/backend/internal/model"
	"learning_buddy/backend/internal/service"
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
	items, err := h.Svc.Repos.ListVisibleMaterials(c.Request.Context(), visible, teamID, q, limit)
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
		m, err := h.Svc.Materials.Create(c.Request.Context(), uid, role, in)
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
	role := middleware.CtxRole(c)
	id, err := bindID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效资料"})
		return
	}
	m, err := h.Svc.Repos.GetMaterial(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "资料不存在"})
		return
	}
	// 可见性校验
	ok, err := h.materialVisible(c, uid, role, m)
	if err != nil || !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问该资料"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"material": m})
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
		if errors.Is(err, middleware.ErrForbidden) {
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
		if errors.Is(err, middleware.ErrForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权删除该资料"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// materialVisible 判断当前用户能否查看该资料（权限在 repository 层原则下，此处做视图级校验）。
func (h *Handlers) materialVisible(c *gin.Context, uid int64, role string, m *model.Material) (bool, error) {
	team, err := h.Svc.Repos.GetTeam(c.Request.Context(), m.TeamID)
	if err != nil {
		return false, err
	}
	if role == "super_admin" {
		return true, nil
	}
	if team.Type == "private" && team.OwnerID != nil && *team.OwnerID == uid {
		return true, nil
	}
	if team.Type == "teacher" {
		// owner 可见全部；学生仅可见 shared
		if team.OwnerID != nil && *team.OwnerID == uid {
			return true, nil
		}
		return m.Shared, nil
	}
	if team.Type == "public" {
		return true, nil
	}
	return false, nil
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
	// 文件上传
	file, err := c.FormFile("file")
	if err == nil && file != nil {
		sk, content := saveUpload(h.Svc.Cfg.UploadDir, file)
		in.StorageKey = &sk
		if in.Content == nil && content != nil {
			in.Content = content
		}
		if in.FileType == nil {
			ext := strings.TrimPrefix(filepath.Ext(file.Filename), ".")
			ft := ext
			in.FileType = &ft
		}
	}
	return in
}

// saveUpload 保存上传文件到 UPLOAD_DIR，返回 storage_key 与（文本类文件的）内容。
func saveUpload(dir string, file *multipart.FileHeader) (string, *string) {
	_ = os.MkdirAll(dir, 0o750)
	ext := filepath.Ext(file.Filename)
	key := uuid.NewString() + ext
	dst := filepath.Join(dir, key)
	src, _ := file.Open()
	defer func() { _ = src.Close() }()
	out, _ := os.Create(dst) // #nosec G304 -- dst 由 uuid 生成，非用户输入
	defer func() { _ = out.Close() }()
	_, _ = out.ReadFrom(src)

	var content *string
	lower := strings.ToLower(ext)
	if lower == ".txt" || lower == ".md" {
		b, err := os.ReadFile(dst) // #nosec G304 -- dst 由 uuid 生成
		if err == nil {
			s := string(b)
			content = &s
		}
	}
	return key, content
}

func bindIDStr(s string) (int64, error) {
	var id int64
	_, err := fmt.Sscanf(s, "%d", &id)
	return id, err
}
