package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"learning_buddy/backend/internal/model"
)

// ---- 学习记录 / 练习（F6 / F8）----

func (r *Repositories) CreateLearningRecord(ctx context.Context, rec *model.LearningRecord) error {
	return r.DB.WithContext(ctx).Create(rec).Error
}

func (r *Repositories) ListLearningRecords(ctx context.Context, userID int64) ([]model.LearningRecord, error) {
	var items []model.LearningRecord
	if err := r.DB.WithContext(ctx).Where("user_id = ?", userID).Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repositories) CreateQuizAttempt(ctx context.Context, a *model.QuizAttempt) error {
	return r.DB.WithContext(ctx).Create(a).Error
}

func (r *Repositories) ListQuizAttempts(ctx context.Context, userID int64) ([]model.QuizAttempt, error) {
	var items []model.QuizAttempt
	if err := r.DB.WithContext(ctx).Where("user_id = ?", userID).Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repositories) CreateExercise(ctx context.Context, e *model.Exercise) error {
	return r.DB.WithContext(ctx).Create(e).Error
}

func (r *Repositories) GetExerciseForUser(
	ctx context.Context,
	exerciseID int64,
	userID int64,
) (*model.Exercise, error) {
	var exercise model.Exercise
	result := r.DB.WithContext(ctx).
		Where("id = ? AND user_id = ?", exerciseID, userID).
		Limit(1).
		Find(&exercise)
	if result.Error != nil {
		return nil, fmt.Errorf("get exercise for user: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, ErrNotFound
	}
	return &exercise, nil
}

func (r *Repositories) ListExercises(ctx context.Context, materialID *int64) ([]model.Exercise, error) {
	var items []model.Exercise
	q := r.DB.WithContext(ctx).Model(&model.Exercise{})
	if materialID != nil {
		q = q.Where("material_id = ?", *materialID)
	}
	if err := q.Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// ---- 学习计划（F7）----

func (r *Repositories) CreateStudyPlan(ctx context.Context, p *model.StudyPlan) error {
	return r.DB.WithContext(ctx).Create(p).Error
}

func (r *Repositories) ListStudyPlans(ctx context.Context, userID int64) ([]model.StudyPlan, error) {
	var items []model.StudyPlan
	if err := r.DB.WithContext(ctx).Where("user_id = ?", userID).Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repositories) GetStudyPlan(ctx context.Context, id, userID int64) (*model.StudyPlan, error) {
	var p model.StudyPlan
	if err := r.DB.WithContext(ctx).First(&p, "id = ? AND user_id = ?", id, userID).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repositories) UpdateStudyPlan(ctx context.Context, p *model.StudyPlan) error {
	return r.DB.WithContext(ctx).Save(p).Error
}

// ---- 阅读笔记（F3）----

func (r *Repositories) CreateNoteForVisibleMaterial(ctx context.Context, n *model.MaterialNote) error {
	if n == nil {
		return fmt.Errorf("create note for visible material: note is nil")
	}
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := New(tx)
		if _, err := txRepo.GetVisibleMaterial(ctx, n.UserID, n.MaterialID); err != nil {
			return fmt.Errorf("check note material visibility: %w", err)
		}
		if err := tx.Create(n).Error; err != nil {
			return fmt.Errorf("create material note: %w", err)
		}
		return nil
	})
}

func (r *Repositories) ListNotesForVisibleMaterial(ctx context.Context, userID, materialID int64) ([]model.MaterialNote, error) {
	if _, err := r.GetVisibleMaterial(ctx, userID, materialID); err != nil {
		return nil, fmt.Errorf("check notes material visibility: %w", err)
	}
	var items []model.MaterialNote
	if err := r.DB.WithContext(ctx).Where("user_id = ? AND material_id = ?", userID, materialID).Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list material notes: %w", err)
	}
	return items, nil
}

func (r *Repositories) UpdateNote(ctx context.Context, id, userID int64, content string) error {
	res := r.DB.WithContext(ctx).Model(&model.MaterialNote{}).
		Where("id = ? AND user_id = ?", id, userID).
		Updates(map[string]interface{}{"content": content})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repositories) DeleteNote(ctx context.Context, id, userID int64) error {
	res := r.DB.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&model.MaterialNote{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- Token 用量（F8 限流/额度）----

func (r *Repositories) RecordTokenUsage(ctx context.Context, u *model.TokenUsage) error {
	return r.DB.WithContext(ctx).Create(u).Error
}

func (r *Repositories) DailyTokenUsage(ctx context.Context, userID int64, service string) (int, error) {
	var total int
	q := r.DB.WithContext(ctx).Model(&model.TokenUsage{}).
		Where("user_id = ? AND created_at >= now() - interval '1 day'", userID)
	if service != "" {
		q = q.Where("service = ?", service)
	}
	if err := q.Select("COALESCE(SUM(total_tokens),0)").Scan(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}
