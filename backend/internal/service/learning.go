package service

import (
	"context"

	"learning_buddy/backend/internal/model"
	"learning_buddy/backend/internal/repository"
)

// LearningService 学习记录（F6）：时长/进度/成绩落库 + 进度聚合。
type LearningService struct {
	repos *repository.Repositories
}

func NewLearningService(repos *repository.Repositories) *LearningService {
	return &LearningService{repos: repos}
}

// Record 写入一条学习记录（行级隔离：user_id 必填）。
func (s *LearningService) Record(ctx context.Context, userID int64, materialID *int64, durationS int, progress float64, score *float64) (*model.LearningRecord, error) {
	rec := &model.LearningRecord{
		UserID:     userID,
		MaterialID: materialID,
		DurationS:  durationS,
		Progress:   progress,
		Score:      score,
	}
	if err := s.repos.CreateLearningRecord(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// List 我的学习记录。
func (s *LearningService) List(ctx context.Context, userID int64) ([]model.LearningRecord, error) {
	return s.repos.ListLearningRecords(ctx, userID)
}

// ProgressSummary 进度看板聚合（F9）：总时长、平均完成度、测验正确率、按日趋势。
type ProgressSummary struct {
	TotalDurationS int             `json:"total_duration_s"`
	AvgProgress    float64         `json:"avg_progress"`
	QuizCount      int             `json:"quiz_count"`
	QuizCorrect    int             `json:"quiz_correct"`
	QuizAccuracy   float64         `json:"quiz_accuracy"`
	Daily          []DailyProgress `json:"daily"`
}

// DailyProgress 单日进度。
type DailyProgress struct {
	Date     string  `json:"date"`
	Duration int     `json:"duration_s"`
	Progress float64 `json:"avg_progress"`
}

// Summary 计算进度看板数据。
func (s *LearningService) Summary(ctx context.Context, userID int64) (*ProgressSummary, error) {
	records, err := s.repos.ListLearningRecords(ctx, userID)
	if err != nil {
		return nil, err
	}
	attempts, err := s.repos.ListQuizAttempts(ctx, userID)
	if err != nil {
		return nil, err
	}
	sum := &ProgressSummary{}
	for _, r := range records {
		sum.TotalDurationS += r.DurationS
		sum.AvgProgress += r.Progress
	}
	if len(records) > 0 {
		sum.AvgProgress = sum.AvgProgress / float64(len(records))
	}
	for _, a := range attempts {
		sum.QuizCount++
		if a.IsCorrect != nil && *a.IsCorrect {
			sum.QuizCorrect++
		}
	}
	if sum.QuizCount > 0 {
		sum.QuizAccuracy = float64(sum.QuizCorrect) / float64(sum.QuizCount) * 100
	}
	// 按日聚合（最近 14 天）
	dailyMap := map[string]*DailyProgress{}
	for _, r := range records {
		d := r.CreatedAt.Format("2006-01-02")
		if _, ok := dailyMap[d]; !ok {
			dailyMap[d] = &DailyProgress{Date: d}
		}
		dailyMap[d].Duration += r.DurationS
		dailyMap[d].Progress += r.Progress
	}
	for _, dp := range dailyMap {
		if c := countByDate(records, dp.Date); c > 0 {
			dp.Progress = dp.Progress / float64(c)
		}
	}
	for _, dp := range dailyMap {
		sum.Daily = append(sum.Daily, *dp)
	}
	return sum, nil
}

func countByDate(records []model.LearningRecord, date string) int {
	n := 0
	for _, r := range records {
		if r.CreatedAt.Format("2006-01-02") == date {
			n++
		}
	}
	return n
}
