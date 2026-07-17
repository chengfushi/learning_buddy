// package repository —— RAG 追踪、资产和反馈数据访问；资料访问继续复用统一可见性谓词。
package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"learning_buddy/backend/internal/model"
)

// RecordRAGRun 原子保存运行记录与候选命中。
func (r *Repositories) RecordRAGRun(
	ctx context.Context,
	run *model.RAGRun,
	hits []model.RAGRunHit,
) error {
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(run).Error; err != nil {
			return fmt.Errorf("create RAG run: %w", err)
		}
		if len(hits) > 0 {
			if err := tx.Create(&hits).Error; err != nil {
				return fmt.Errorf("create RAG run hits: %w", err)
			}
		}
		return nil
	})
}

// UpsertMessageFeedback 仅允许会话所有者评价自己的助手消息。
func (r *Repositories) UpsertMessageFeedback(
	ctx context.Context,
	userID int64,
	messageID int64,
	rating string,
	reason *string,
) (*model.MessageFeedback, error) {
	var count int64
	err := r.DB.WithContext(ctx).
		Table("agent_messages AS am").
		Joins("JOIN agent_sessions AS s ON s.id = am.session_id").
		Where("am.id = ? AND am.role = ? AND s.user_id = ?", messageID, "assistant", userID).
		Count(&count).Error
	if err != nil {
		return nil, fmt.Errorf("authorize message feedback: %w", err)
	}
	if count != 1 {
		return nil, ErrNotFound
	}
	feedback := &model.MessageFeedback{
		MessageID: messageID, UserID: userID, Rating: rating, Reason: reason,
	}
	err = r.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "message_id"}, {Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"rating": rating, "reason": reason, "updated_at": gorm.Expr("now()"),
		}),
	}).Create(feedback).Error
	if err != nil {
		return nil, fmt.Errorf("upsert message feedback: %w", err)
	}
	result := r.DB.WithContext(ctx).
		Where("message_id = ? AND user_id = ?", messageID, userID).
		Limit(1).
		Find(feedback)
	if result.Error != nil {
		return nil, fmt.Errorf("reload message feedback: %w", result.Error)
	}
	return feedback, nil
}

// ListVisibleMaterialAssets 在资料权限校验后返回派生图片元数据。
func (r *Repositories) ListVisibleMaterialAssets(
	ctx context.Context,
	userID int64,
	materialID int64,
) ([]model.MaterialAsset, error) {
	material, err := r.GetVisibleMaterial(ctx, userID, materialID)
	if err != nil {
		return nil, err
	}
	var assets []model.MaterialAsset
	if err := r.DB.WithContext(ctx).
		Where("material_id = ? AND index_version = ?", materialID, material.IndexVersion).
		Order("page_number NULLS LAST, id").
		Find(&assets).Error; err != nil {
		return nil, fmt.Errorf("list visible material assets: %w", err)
	}
	return assets, nil
}

// GetVisibleProcessingRun 复用资料可见性后返回当前代次的解析进度。
func (r *Repositories) GetVisibleProcessingRun(
	ctx context.Context,
	userID int64,
	materialID int64,
) (*model.RAGProcessingRun, error) {
	material, err := r.GetVisibleMaterial(ctx, userID, materialID)
	if err != nil {
		return nil, err
	}
	var run model.RAGProcessingRun
	result := r.DB.WithContext(ctx).
		Where("material_id = ? AND parse_generation = ?", materialID, material.ParseGeneration).
		Order("started_at DESC").Limit(1).Find(&run)
	if result.Error != nil {
		return nil, fmt.Errorf("get visible processing run: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return &run, nil
}
