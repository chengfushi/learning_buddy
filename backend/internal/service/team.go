// package service —— 业务逻辑、RBAC、可见 team 计算。不直接拼资料权限 SQL。
package service

import (
	"context"

	"learning_buddy/backend/internal/repository"
)

// Services 聚合所有 service。
type Services struct {
	Repos *repository.Repositories
	Teams *TeamService
}

func New(repos *repository.Repositories) *Services {
	return &Services{Repos: repos, Teams: NewTeamService(repos)}
}

// TeamService 处理 team / 可见集合计算。
type TeamService struct{ repos *repository.Repositories }

func NewTeamService(repos *repository.Repositories) *TeamService { return &TeamService{repos: repos} }

// ComputeVisibleTeamIDs 计算用户可见 team 集合：
// 私人 team + 已 approved 加入的 teacher team + 公共库(特判，不查 team_members)。
// 结果交给 repository.VisibleMaterialsScope 使用。
func (s *TeamService) ComputeVisibleTeamIDs(ctx context.Context, userID int64) ([]int64, error) {
	// 实现示例（占位）：
	// 1. 取用户私有 team（type='private' 且 owner_id=userID）
	// 2. 取 team_members 中 status='approved' 的 teacher team
	// 3. 追加 type='public' 的团队（无需 member 行）
	return []int64{}, nil
}
