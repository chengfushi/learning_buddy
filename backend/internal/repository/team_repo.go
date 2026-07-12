package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"learning_buddy/backend/internal/model"
)

// GetTeam 按 ID 取团队。
func (r *Repositories) GetTeam(ctx context.Context, id int64) (*model.Team, error) {
	var t model.Team
	if err := r.DB.WithContext(ctx).First(&t, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
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

// ListTeamMaterials 列出某 team 内「当前查看者可见」的资料（F2.2 资料可见性）：
//   - owner / super_admin：看到该 team 全部资料（含备课草稿）
//   - 其他查看者（如已加入的学生）：teacher team 仅 shared=true，其余类型全部
//
// 权限过滤仅在 repository 层拼装，杜绝 Agent/前端重拼谓词（R2）。
func (r *Repositories) ListTeamMaterials(ctx context.Context, teamID int64, viewerUserID int64, viewerRole string) ([]model.Material, error) {
	var items []model.Material
	q := r.DB.WithContext(ctx).Model(&model.Material{}).Where("team_id = ?", teamID)
	isOwner := false
	var t model.Team
	if err := r.DB.WithContext(ctx).First(&t, "id = ?", teamID).Error; err == nil {
		if t.OwnerID != nil && *t.OwnerID == viewerUserID {
			isOwner = true
		}
	}
	if t.Type == "teacher" && !isOwner && viewerRole != "super_admin" {
		q = q.Where("shared = ?", true)
	}
	if err := q.Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}
