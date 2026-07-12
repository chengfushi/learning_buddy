package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"

	"learning_buddy/backend/internal/model"
	"learning_buddy/backend/internal/repository"
)

// TeamService 处理 team / 可见集合计算 / 成员审批（F2.1/F2.3/F2.5）。
type TeamService struct {
	repos *repository.Repositories
}

func NewTeamService(repos *repository.Repositories) *TeamService {
	return &TeamService{repos: repos}
}

// ErrForbidden 通用越权错误。
var ErrForbidden = errors.New("forbidden")

// VisibleTeamIDs 计算用户可见 team 集合（委托 repository，权限真源在此）。
func (s *TeamService) VisibleTeamIDs(ctx context.Context, userID int64) ([]int64, error) {
	return s.repos.VisibleTeamIDs(ctx, userID)
}

// MyTeams 返回用户可见的所有 team（私人 + 已加入 teacher + 公共），附带角色标注。
func (s *TeamService) MyTeams(ctx context.Context, userID int64) ([]model.Team, error) {
	ids, err := s.repos.VisibleTeamIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []model.Team{}, nil
	}
	var teams []model.Team
	if err := s.repos.DB.WithContext(ctx).Where("id IN ?", ids).Order("type, id").Find(&teams).Error; err != nil {
		return nil, err
	}
	return teams, nil
}

// CreateTeacherTeam 老师创建学习小组 team，生成唯一 join_code（F2.1）。
func (s *TeamService) CreateTeacherTeam(ctx context.Context, ownerID int64, name string) (*model.Team, error) {
	code, err := s.uniqueJoinCode(ctx)
	if err != nil {
		return nil, err
	}
	t := &model.Team{
		Name:     name,
		Type:     "teacher",
		JoinCode: &code,
		OwnerID:  &ownerID,
	}
	if err := s.repos.DB.WithContext(ctx).Create(t).Error; err != nil {
		return nil, err
	}
	return t, nil
}

// JoinByCode 学生凭 join_code 申请加入 teacher team → pending（F2.5）。
func (s *TeamService) JoinByCode(ctx context.Context, userID int64, code string) (*model.TeamMember, error) {
	t, err := s.repos.TeamByJoinCode(ctx, code)
	if err != nil {
		return nil, errors.New("班级码无效")
	}
	if t.Type != "teacher" {
		return nil, errors.New("该团队不支持凭码加入")
	}
	// 幂等：已存在成员关系则直接返回
	var existing model.TeamMember
	err = s.repos.DB.WithContext(ctx).First(&existing, "team_id = ? AND user_id = ?", t.ID, userID).Error
	if err == nil {
		return &existing, nil
	}
	m := model.TeamMember{
		TeamID: t.ID,
		UserID: userID,
		Role:   "member",
		Status: "pending",
	}
	if err := s.repos.DB.WithContext(ctx).Create(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// ApproveMember 老师（team owner）审批学生加入 → approved（F2.5）。
func (s *TeamService) ApproveMember(ctx context.Context, teamID, ownerID, targetUserID int64) error {
	t, err := s.getTeam(ctx, teamID)
	if err != nil {
		return err
	}
	if t.OwnerID == nil || *t.OwnerID != ownerID {
		return ErrForbidden
	}
	res := s.repos.DB.WithContext(ctx).
		Model(&model.TeamMember{}).
		Where("team_id = ? AND user_id = ?", teamID, targetUserID).
		Update("status", "approved")
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("成员关系不存在")
	}
	return nil
}

// ListMembers 返回团队成员与待审批列表（仅 owner 可见，F2.5）。
func (s *TeamService) ListMembers(ctx context.Context, teamID, ownerID int64) ([]model.TeamMember, error) {
	t, err := s.getTeam(ctx, teamID)
	if err != nil {
		return nil, err
	}
	if t.OwnerID == nil || *t.OwnerID != ownerID {
		return nil, ErrForbidden
	}
	var members []model.TeamMember
	if err := s.repos.DB.WithContext(ctx).Where("team_id = ?", teamID).Order("status, joined_at").Find(&members).Error; err != nil {
		return nil, err
	}
	return members, nil
}

func (s *TeamService) getTeam(ctx context.Context, id int64) (*model.Team, error) {
	var t model.Team
	if err := s.repos.DB.WithContext(ctx).First(&t, "id = ?", id).Error; err != nil {
		return nil, repository.ErrNotFound
	}
	return &t, nil
}

// uniqueJoinCode 生成 6 位唯一班级码。
func (s *TeamService) uniqueJoinCode(ctx context.Context) (string, error) {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	for i := 0; i < 10; i++ {
		b := make([]byte, 6)
		for j := range b {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
			if err != nil {
				return "", err
			}
			b[j] = charset[n.Int64()]
		}
		code := string(b)
		var cnt int64
		s.repos.DB.WithContext(ctx).Model(&model.Team{}).Where("join_code = ?", code).Count(&cnt)
		if cnt == 0 {
			return code, nil
		}
	}
	return "", fmt.Errorf("生成班级码失败")
}
