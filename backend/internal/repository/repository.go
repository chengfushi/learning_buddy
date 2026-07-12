// package repository —— 仅此层拼 SQL / GORM。
// 权限铁律（engineering-standards.md §0）：任何「用户可见资料范围」的逻辑只能写在这里，
// Agent 与前端不拼权限谓词。违反视为严重缺陷。
package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"learning_buddy/backend/internal/model"
)

// Repositories 聚合所有 repository。
type Repositories struct {
	DB *gorm.DB
}

func New(db *gorm.DB) *Repositories { return &Repositories{DB: db} }

// ---- 权限核心（R2）----

// VisibleTeamIDs 计算用户可见 team 集合（见 docs/database.md §4）：
//  1. 私人 team（type='private' 且 owner_id = userID）
//  2. 已 approved 加入的 teacher team（经 team_members）
//  3. 公共库（type='public'，特判，不查 team_members，避免成员表膨胀）
//
// 结果交给 VisibleMaterialsScope 使用，是「用户可见资料范围」的唯一真源。
func (r *Repositories) VisibleTeamIDs(ctx context.Context, userID int64) ([]int64, error) {
	var ids []int64
	err := r.DB.WithContext(ctx).
		Table("teams").
		Select("id").
		Where("type = ? AND owner_id = ?", "private", userID).
		Or("type = ?", "public").
		Or("id IN (?)",
			r.DB.Table("team_members tm").
				Select("tm.team_id").
				Joins("JOIN teams t ON t.id = tm.team_id").
				Where("tm.user_id = ? AND tm.status = ? AND t.type = ?", userID, "approved", "teacher")).
		Pluck("id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// VisibleMaterialsScope 是「用户可见资料」的唯一拼装点（与 docs/database.md §4 严格对应）。
// 谓词：team_id 在可见集合内，且 teacher team 仅取 shared=true（其余类型一律可见）。
// 关键：student 对 teacher team 中 shared=false 的备课草稿，此谓词天然排除——越权路径被物理隔绝。
func (r *Repositories) VisibleMaterialsScope(teamIDs []int64) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.
			Joins("JOIN teams ON teams.id = materials.team_id").
			Where("materials.team_id IN ?", teamIDs).
			Where("teams.type <> ? OR materials.shared = ?", "teacher", true)
	}
}

// ListVisibleMaterials 在「用户可见 team 集合」内列出资料；权限隔离在此强制生效。
func (r *Repositories) ListVisibleMaterials(ctx context.Context, teamIDs []int64, teamID *int64, keyword string, limit int) ([]model.Material, error) {
	var items []model.Material
	q := r.DB.WithContext(ctx).Model(&model.Material{}).Scopes(r.VisibleMaterialsScope(teamIDs))
	if teamID != nil {
		q = q.Where("materials.team_id = ?", *teamID)
	}
	if keyword != "" {
		q = q.Where("materials.title ILIKE ?", "%"+keyword+"%")
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Order("materials.created_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// GetMaterial 按 ID 取资料（不含权限过滤；调用方应先用可见集合校验）。
func (r *Repositories) GetMaterial(ctx context.Context, id int64) (*model.Material, error) {
	var m model.Material
	if err := r.DB.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// TeamByJoinCode 凭班级码取 teacher team（F2.5）。
func (r *Repositories) TeamByJoinCode(ctx context.Context, code string) (*model.Team, error) {
	var t model.Team
	if err := r.DB.WithContext(ctx).First(&t, "join_code = ?", code).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// ErrNotFound 统一未找到错误。
var ErrNotFound = errors.New("record not found")

// GetUser 按 ID 取用户（不含密码哈希外泄）。
func (r *Repositories) GetUser(ctx context.Context, id int64) (*model.User, error) {
	var u model.User
	if err := r.DB.WithContext(ctx).First(&u, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}
