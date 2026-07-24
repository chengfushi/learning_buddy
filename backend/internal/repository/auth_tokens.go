// package repository —— Refresh Token 持久化与轮换，数据库只保存不可逆哈希。
package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"learning_buddy/backend/internal/model"
)

var (
	ErrRefreshTokenNotFound = errors.New("refresh token not found")
	ErrRefreshTokenReplay   = errors.New("refresh token replay detected")
)

func (r *Repositories) CreateRefreshToken(ctx context.Context, token *model.RefreshToken) error {
	if err := r.DB.WithContext(ctx).Create(token).Error; err != nil {
		return err
	}
	return nil
}

func (r *Repositories) FindRefreshToken(ctx context.Context, hash string) (*model.RefreshToken, error) {
	var token model.RefreshToken
	result := r.DB.WithContext(ctx).Where("token_hash = ?", hash).Limit(1).Find(&token)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrRefreshTokenNotFound
	}
	return &token, nil
}

// RotateRefreshToken 在事务中消费旧 token 并创建新 token，避免并发刷新双花。
func (r *Repositories) RotateRefreshToken(ctx context.Context, oldHash string, now time.Time, replacement *model.RefreshToken) error {
	replay := false
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old model.RefreshToken
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("token_hash = ?", oldHash).Limit(1).Find(&old)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrRefreshTokenNotFound
		}
		if old.UsedAt != nil {
			if err := tx.Model(&model.RefreshToken{}).
				Where("family_id = ? AND revoked_at IS NULL", old.FamilyID).
				Updates(map[string]any{"revoked_at": now}).Error; err != nil {
				return err
			}
			replay = true
			return nil
		}
		if old.RevokedAt != nil || !old.ExpiresAt.After(now) {
			return ErrRefreshTokenNotFound
		}
		usedAt := now
		if err := tx.Model(&old).Updates(map[string]any{
			"used_at":          usedAt,
			"replaced_by_hash": replacement.TokenHash,
		}).Error; err != nil {
			return err
		}
		return tx.Create(replacement).Error
	})
	if err != nil {
		return err
	}
	if replay {
		return ErrRefreshTokenReplay
	}
	return nil
}

func (r *Repositories) RevokeRefreshFamily(ctx context.Context, familyID string, now time.Time) error {
	return r.DB.WithContext(ctx).Model(&model.RefreshToken{}).
		Where("family_id = ? AND revoked_at IS NULL", familyID).
		Updates(map[string]any{"revoked_at": now}).Error
}
