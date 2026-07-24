package repository

import (
	"context"
	"fmt"

	"learning_buddy/backend/internal/model"
)

// GetTeam 按 ID 取团队。
func (r *Repositories) GetTeam(ctx context.Context, id int64) (*model.Team, error) {
	var t model.Team
	result := r.DB.WithContext(ctx).Where("id = ?", id).Limit(1).Find(&t)
	if result.Error != nil {
		return nil, fmt.Errorf("get team: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return &t, nil
}

// CanWriteToTeam 写权限校验（repository 层强制，对应 docs 上传权限规则）：
//   - super_admin 可写 public team
//   - private team 仅 owner 学生本人
//   - teacher team 仅 owner 老师
func (r *Repositories) CanWriteToTeam(_ context.Context, userID int64, role string, team *model.Team) (bool, error) {
	if team == nil {
		return false, ErrNotFound
	}
	if role == "super_admin" && team.Type == "public" {
		return true, nil
	}
	if (team.Type == "private" || team.Type == "teacher") && team.OwnerID != nil && *team.OwnerID == userID {
		return true, nil
	}
	return false, nil
}

// ListTeamMaterials 复用统一可见性 scope 列出指定 team 的资料。
func (r *Repositories) ListTeamMaterials(ctx context.Context, teamID, viewerUserID int64) ([]model.Material, error) {
	teamIDs, err := r.VisibleTeamIDs(ctx, viewerUserID)
	if err != nil {
		return nil, err
	}
	return r.ListVisibleMaterials(ctx, viewerUserID, teamIDs, &teamID, "", 0)
}
