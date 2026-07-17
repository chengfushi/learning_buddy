package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"learning_buddy/backend/internal/model"
)

// ClaimedParseTask 是 repository 原子抢占后返回的当前规范任务载荷。
// worker 必须只使用这里的内容与代次，禁止使用 goroutine 启动前捕获的资料快照。
type ClaimedParseTask struct {
	MaterialID int64   `gorm:"column:material_id"`
	Generation int64   `gorm:"column:parse_generation"`
	Content    *string `gorm:"column:content"`
	FileType   *string `gorm:"column:file_type"`
	StorageKey *string `gorm:"column:storage_key"`
}

// MaterialUpdatePatch 只表示请求实际携带的字段，避免用陈旧快照覆盖并发写入。
type MaterialUpdatePatch struct {
	Title   *string
	Content *string
	Shared  *bool
}

// RequeueFailedParse 以条件更新将 failed 任务重新入队。
func (r *Repositories) RequeueFailedParse(ctx context.Context, materialID int64) (bool, error) {
	result := r.DB.WithContext(ctx).Model(&model.Material{}).
		Where("id = ? AND parse_status = ?", materialID, "failed").
		Updates(map[string]any{
			"parse_status":     "pending",
			"parse_error":      nil,
			"parse_generation": gorm.Expr("parse_generation + 1"),
		})
	if result.Error != nil {
		return false, fmt.Errorf("requeue failed material parse: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}

// RequeueInterruptedParses 将进程中断遗留的 parsing 任务恢复为 pending。
func (r *Repositories) RequeueInterruptedParses(ctx context.Context) (int64, error) {
	result := r.DB.WithContext(ctx).Model(&model.Material{}).
		Where("parse_status = ?", "parsing").
		Updates(map[string]any{
			"parse_status":     "pending",
			"parse_error":      nil,
			"parse_generation": gorm.Expr("parse_generation + 1"),
		})
	if result.Error != nil {
		return 0, fmt.Errorf("requeue interrupted parse tasks: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// ListPendingParses 按 ID 获取待派发任务。
func (r *Repositories) ListPendingParses(ctx context.Context, limit int) ([]model.Material, error) {
	var materials []model.Material
	if err := r.DB.WithContext(ctx).
		Where("parse_status = ?", "pending").
		Order("id ASC").
		Limit(limit).
		Find(&materials).Error; err != nil {
		return nil, fmt.Errorf("list pending parse tasks: %w", err)
	}
	return materials, nil
}

// ClaimParseTask 原子抢占 pending 任务并返回同一条 UPDATE 对应的规范载荷。
func (r *Repositories) ClaimParseTask(
	ctx context.Context,
	materialID int64,
) (*ClaimedParseTask, bool, error) {
	var task ClaimedParseTask
	result := r.DB.WithContext(ctx).Raw(
		`UPDATE materials
		 SET parse_status = ?, parse_error = NULL
		 WHERE id = ? AND parse_status = ?
		 RETURNING id AS material_id, parse_generation, content, file_type, storage_key`,
		"parsing",
		materialID,
		"pending",
	).Scan(&task)
	if result.Error != nil {
		return nil, false, fmt.Errorf("claim material parse task: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, false, nil
	}
	return &task, true, nil
}

// UpdateMaterial 在行锁下只更新 patch 指定的字段；正文实际变化时原子递增代次并重新入队。
func (r *Repositories) UpdateMaterial(
	ctx context.Context,
	materialID int64,
	patch MaterialUpdatePatch,
) (*model.Material, bool, error) {
	var updated model.Material
	reparse := false
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.Material
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", materialID).
			Limit(1).
			Find(&current)
		if query.Error != nil {
			return fmt.Errorf("lock material for update: %w", query.Error)
		}
		if query.RowsAffected != 1 {
			return ErrNotFound
		}

		updates := make(map[string]any, 6)
		if patch.Title != nil {
			updates["title"] = *patch.Title
		}
		if patch.Shared != nil {
			updates["shared"] = *patch.Shared
		}
		if patch.Content != nil && (current.Content == nil || *current.Content != *patch.Content) {
			reparse = true
			updates["content"] = *patch.Content
			updates["parse_status"] = "pending"
			updates["parse_error"] = nil
			updates["parse_generation"] = gorm.Expr("parse_generation + 1")
		}
		if len(updates) > 0 {
			result := tx.Model(&model.Material{}).
				Where("id = ?", materialID).
				Updates(updates)
			if result.Error != nil {
				return fmt.Errorf("update material fields: %w", result.Error)
			}
			if result.RowsAffected != 1 {
				return ErrNotFound
			}
		}

		query = tx.Where("id = ?", materialID).Limit(1).Find(&updated)
		if query.Error != nil {
			return fmt.Errorf("reload updated material: %w", query.Error)
		}
		if query.RowsAffected != 1 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return &updated, reparse, nil
}

// FinishParseTask 只完成仍由当前 worker 持有的 parsing 任务。
func (r *Repositories) FinishParseTask(
	ctx context.Context,
	materialID int64,
	generation int64,
	status string,
	parseError any,
) (bool, error) {
	result := r.DB.WithContext(ctx).Model(&model.Material{}).
		Where(
			"id = ? AND parse_status = ? AND parse_generation = ?",
			materialID,
			"parsing",
			generation,
		).
		Updates(map[string]any{"parse_status": status, "parse_error": parseError})
	if result.Error != nil {
		return false, fmt.Errorf("finish material parse task: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}
