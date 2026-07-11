// package repository —— 仅此层拼 SQL / GORM。
// 权限铁律：任何「用户可见资料范围」的逻辑只能写在这里，Agent 与前端不拼谓词。
package repository

import (
	"gorm.io/gorm"
)

// Material 对应 materials 表。
type Material struct {
	ID          int64 `gorm:"primaryKey"`
	TeamID      int64 `gorm:"column:team_id"`
	Title       string
	Shared      bool
	ParseStatus string `gorm:"column:parse_status"`
}

// Repositories 聚合所有 repository。
type Repositories struct {
	DB *gorm.DB
}

func New(db *gorm.DB) *Repositories { return &Repositories{DB: db} }

// VisibleMaterialsScope 是「用户可见资料」的唯一拼装点。
// 谓词：team_id 在可见集合内，且 teacher team 仅取 shared=true（其余类型一律可见）。
// 与 docs/database.md §4 的 SQL 谓词严格对应。
func VisibleMaterialsScope(visibleTeamIDs []int64) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.
			Joins("JOIN teams ON teams.id = materials.team_id").
			Where("materials.team_id IN ?", visibleTeamIDs).
			Where("teams.type <> ? OR materials.shared = ?", "teacher", true)
	}
}

// ListVisible 在「用户可见 team 集合」内列出资料；权限隔离在此强制生效。
func (r *Repositories) ListVisible(teamIDs []int64, keyword string, limit int) ([]Material, error) {
	var items []Material
	q := r.DB.Scopes(VisibleMaterialsScope(teamIDs))
	if keyword != "" {
		q = q.Where("materials.title ILIKE ?", "%"+keyword+"%")
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}
