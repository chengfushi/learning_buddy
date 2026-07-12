package service

import (
	"context"
	"errors"
	"log/slog"

	"learning_buddy/backend/internal/config"
	"learning_buddy/backend/internal/model"
	"learning_buddy/backend/internal/repository"
)

// MaterialService 资料 CRUD（F2.2/2.4）+ shared 可见性 + 触发解析。
// 写权限与 shared 规则强制在 repository 层（见 CanWriteToTeam）。
type MaterialService struct {
	repos *repository.Repositories
	cfg   *config.Config
	agent *AgentService
}

func NewMaterialService(repos *repository.Repositories, cfg *config.Config, agent *AgentService) *MaterialService {
	return &MaterialService{repos: repos, cfg: cfg, agent: agent}
}

// CreateInput 创建资料入参。
type CreateInput struct {
	TeamID     int64
	Title      string
	Subject    *string
	Chapter    *string
	Tags       model.StringArray
	Content    *string
	FileType   *string
	StorageKey *string
	OwnerID    int64
}

// Create 创建资料，校验写权限后落库，并异步触发解析（F2 基座：上传即解析）。
func (s *MaterialService) Create(ctx context.Context, userID int64, role string, in CreateInput) (*model.Material, error) {
	team, err := s.repos.GetTeam(ctx, in.TeamID)
	if err != nil {
		return nil, err
	}
	ok, err := s.repos.CanWriteToTeam(ctx, userID, role, team)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}
	m := &model.Material{
		TeamID:      in.TeamID,
		Title:       in.Title,
		Subject:     in.Subject,
		Chapter:     in.Chapter,
		Tags:        in.Tags,
		Content:     in.Content,
		FileType:    in.FileType,
		StorageKey:  in.StorageKey,
		ParseStatus: "pending",
		Shared:      false,
		OwnerID:     in.OwnerID,
	}
	if err := s.repos.DB.WithContext(ctx).Create(m).Error; err != nil {
		return nil, err
	}
	// 异步触发解析（不阻塞请求；Agent 负责写 chunks 并回写 parse_status）
	go s.triggerParse(m, in.Content)
	return m, nil
}

// Update 更新资料 / 切 shared（F2.2）。shared 仅 teacher team 生效。
func (s *MaterialService) Update(ctx context.Context, userID int64, role string, materialID int64, title *string, content *string, shared *bool) (*model.Material, error) {
	m, err := s.repos.GetMaterial(ctx, materialID)
	if err != nil {
		return nil, err
	}
	team, err := s.repos.GetTeam(ctx, m.TeamID)
	if err != nil {
		return nil, err
	}
	ok, err := s.repos.CanWriteToTeam(ctx, userID, role, team)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}
	// shared 仅 teacher team 的材料可设置；其余类型忽略/拒绝
	if shared != nil {
		if team.Type != "teacher" {
			return nil, errors.New("仅老师小组的资料可设置 shared 可见性")
		}
		m.Shared = *shared
	}
	if title != nil {
		m.Title = *title
	}
	reparse := false
	if content != nil && (m.Content == nil || *m.Content != *content) {
		m.Content = content
		reparse = true
	}
	if err := s.repos.DB.WithContext(ctx).Save(m).Error; err != nil {
		return nil, err
	}
	if reparse {
		go s.triggerParse(m, m.Content)
	}
	return m, nil
}

// Delete 删除资料（级联删 chunks，FK ON DELETE CASCADE）。
func (s *MaterialService) Delete(ctx context.Context, userID int64, role string, materialID int64) error {
	m, err := s.repos.GetMaterial(ctx, materialID)
	if err != nil {
		return err
	}
	team, err := s.repos.GetTeam(ctx, m.TeamID)
	if err != nil {
		return err
	}
	ok, err := s.repos.CanWriteToTeam(ctx, userID, role, team)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	return s.repos.DB.WithContext(ctx).Delete(&model.Material{}, materialID).Error
}

// triggerParse 异步触发 Agent 解析（切分/嵌入/写 chunks/回写状态）。
func (s *MaterialService) triggerParse(m *model.Material, content *string) {
	ctx := context.Background()
	// 标记 parsing
	_ = s.repos.DB.WithContext(ctx).Model(&model.Material{}).
		Where("id = ? AND parse_status = ?", m.ID, "pending").
		Update("parse_status", "parsing").Error
	var text string
	if content != nil {
		text = *content
	}
	err := s.agent.Parse(ctx, m.ID, text, derefStr(m.FileType), derefStr(m.StorageKey))
	if err != nil {
		slog.Warn("material parse trigger failed", "material_id", m.ID, "err", err)
		_ = s.repos.DB.WithContext(ctx).Model(&model.Material{}).
			Where("id = ?", m.ID).Updates(map[string]interface{}{
			"parse_status": "failed",
			"parse_error":  err.Error(),
		}).Error
	}
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
