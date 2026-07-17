package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"learning_buddy/backend/internal/model"
	"learning_buddy/backend/internal/repository"
)

// MaterialService 资料 CRUD（F2.2/2.4）+ shared 可见性 + 触发解析。
// 写权限与 shared 规则强制在 repository 层（见 CanWriteToTeam）。
type MaterialService struct {
	repos        *repository.Repositories
	agent        materialParser
	parseAlerter parseFailureAlerter
	parsePolicy  parseRetryPolicy
}

type materialParser interface {
	Parse(
		ctx context.Context,
		materialID int64,
		generation int64,
		content string,
		fileType string,
		storageKey string,
	) error
}

type parseRetryPolicy struct {
	maxAttempts  int
	timeout      time.Duration
	baseDelay    time.Duration
	dbTimeout    time.Duration
	scanInterval time.Duration
	alertTimeout time.Duration
	batchSize    int
}

var defaultParseRetryPolicy = parseRetryPolicy{
	maxAttempts:  3,
	timeout:      120 * time.Second,
	baseDelay:    time.Second,
	dbTimeout:    5 * time.Second,
	scanInterval: 5 * time.Second,
	alertTimeout: 5 * time.Second,
	batchSize:    100,
}

// ErrMaterialParseNotFailed 表示资料当前不处于可重试的失败状态。
var ErrMaterialParseNotFailed = errors.New("material parse status is not failed")

func NewMaterialService(repos *repository.Repositories, agent *AgentService, alertWebhookURL string) *MaterialService {
	return &MaterialService{
		repos:        repos,
		agent:        agent,
		parseAlerter: newParseFailureAlerter(alertWebhookURL),
		parsePolicy:  defaultParseRetryPolicy,
	}
}

// CreateInput 创建资料入参。
type CreateInput struct {
	TeamID     int64
	Title      string
	Subject    *string
	Chapter    *string
	Tags       model.StringArray
	Content    *string
	FileType   *string
	StorageKey *string
	OwnerID    int64
}

// Create 创建资料，校验写权限后落库，并异步触发解析（F2 基座：上传即解析）。
func (s *MaterialService) Create(ctx context.Context, userID int64, role string, in CreateInput) (*model.Material, error) {
	team, err := s.repos.GetTeam(ctx, in.TeamID)
	if err != nil {
		return nil, err
	}
	ok, err := s.repos.CanWriteToTeam(ctx, userID, role, team)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}
	m := &model.Material{
		TeamID:          in.TeamID,
		Title:           in.Title,
		Subject:         in.Subject,
		Chapter:         in.Chapter,
		Tags:            in.Tags,
		Content:         in.Content,
		FileType:        in.FileType,
		StorageKey:      in.StorageKey,
		ParseStatus:     "pending",
		ParseGeneration: 1,
		Shared:          false,
		OwnerID:         in.OwnerID,
	}
	if err := s.repos.DB.WithContext(ctx).Create(m).Error; err != nil {
		return nil, err
	}
	// 异步触发解析（不阻塞请求；Agent 负责写 chunks 并回写 parse_status）
	go s.triggerParse(context.WithoutCancel(ctx), m.ID)
	return m, nil
}

// Update 更新资料 / 切 shared（F2.2）。shared 仅 teacher team 生效。
func (s *MaterialService) Update(ctx context.Context, userID int64, role string, materialID int64, title *string, content *string, shared *bool) (*model.Material, error) {
	m, err := s.repos.GetMaterial(ctx, materialID)
	if err != nil {
		return nil, err
	}
	team, err := s.repos.GetTeam(ctx, m.TeamID)
	if err != nil {
		return nil, err
	}
	ok, err := s.repos.CanWriteToTeam(ctx, userID, role, team)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}
	// shared 仅 teacher team 的材料可设置；其余类型忽略/拒绝
	if shared != nil {
		if team.Type != "teacher" {
			return nil, errors.New("仅老师小组的资料可设置 shared 可见性")
		}
	}
	updated, reparse, err := s.repos.UpdateMaterial(ctx, materialID, repository.MaterialUpdatePatch{
		Title: title, Content: content, Shared: shared,
	})
	if err != nil {
		return nil, err
	}
	if reparse {
		go s.triggerParse(context.WithoutCancel(ctx), updated.ID)
	}
	return updated, nil
}

// Delete 删除资料（级联删 chunks，FK ON DELETE CASCADE）。
func (s *MaterialService) Delete(ctx context.Context, userID int64, role string, materialID int64) error {
	m, err := s.repos.GetMaterial(ctx, materialID)
	if err != nil {
		return err
	}
	team, err := s.repos.GetTeam(ctx, m.TeamID)
	if err != nil {
		return err
	}
	ok, err := s.repos.CanWriteToTeam(ctx, userID, role, team)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	return s.repos.DB.WithContext(ctx).Delete(&model.Material{}, materialID).Error
}

// RetryParse 将失败的解析任务重新入队；条件更新保证并发请求只有一个成功。
func (s *MaterialService) RetryParse(
	ctx context.Context,
	userID int64,
	role string,
	materialID int64,
) (*model.Material, error) {
	dbCtx, cancel := context.WithTimeout(ctx, s.parsePolicy.dbTimeout)
	defer cancel()

	m, err := s.repos.GetMaterial(dbCtx, materialID)
	if err != nil {
		return nil, fmt.Errorf("get material for parse retry: %w", err)
	}
	team, err := s.repos.GetTeam(dbCtx, m.TeamID)
	if err != nil {
		return nil, fmt.Errorf("get team for parse retry: %w", err)
	}
	canWrite, err := s.repos.CanWriteToTeam(dbCtx, userID, role, team)
	if err != nil {
		return nil, fmt.Errorf("check parse retry permission: %w", err)
	}
	if !canWrite {
		return nil, ErrForbidden
	}

	requeued, err := s.repos.RequeueFailedParse(dbCtx, materialID)
	if err != nil {
		return nil, err
	}
	if !requeued {
		return nil, ErrMaterialParseNotFailed
	}

	m.ParseStatus = "pending"
	m.ParseError = nil
	m.ParseGeneration++
	go s.triggerParse(context.WithoutCancel(ctx), m.ID)
	return m, nil
}

// RecoverParseTasks 将进程退出时遗留的 parsing 任务重新入队，并派发所有持久化 pending 任务。
func (s *MaterialService) RecoverParseTasks(ctx context.Context) error {
	dbCtx, cancel := context.WithTimeout(ctx, s.parsePolicy.dbTimeout)
	count, err := s.repos.RequeueInterruptedParses(dbCtx)
	cancel()
	if err != nil {
		return err
	}
	if count > 0 {
		slog.Info("requeued interrupted parse tasks", "count", count)
	}
	return s.DispatchPendingParseTasks(ctx)
}

// RunParseDispatcher 周期扫描数据库中的 pending 任务，补偿进程退出或 goroutine 未启动造成的漏派发。
func (s *MaterialService) RunParseDispatcher(ctx context.Context) {
	ticker := time.NewTicker(s.parsePolicy.scanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.DispatchPendingParseTasks(ctx); err != nil {
				slog.Error("dispatch pending parse tasks", "err", err)
			}
		}
	}
}

// DispatchPendingParseTasks 从持久化状态表批量读取待处理任务；triggerParse 的条件更新负责并发抢占。
func (s *MaterialService) DispatchPendingParseTasks(ctx context.Context) error {
	dbCtx, cancel := context.WithTimeout(ctx, s.parsePolicy.dbTimeout)
	defer cancel()

	materials, err := s.repos.ListPendingParses(dbCtx, s.parsePolicy.batchSize)
	if err != nil {
		return err
	}
	for i := range materials {
		m := materials[i]
		go s.triggerParse(ctx, m.ID)
	}
	if len(materials) > 0 {
		slog.Info("dispatched pending parse tasks", "count", len(materials))
	}
	return nil
}

// triggerParse 抢占并执行解析任务；同一时刻仅允许一个 worker 处理同一资料。
func (s *MaterialService) triggerParse(ctx context.Context, materialID int64) {
	dbCtx, cancel := context.WithTimeout(ctx, s.parsePolicy.dbTimeout)
	task, claimed, err := s.repos.ClaimParseTask(dbCtx, materialID)
	cancel()
	if err != nil {
		slog.Error("claim material parse task failed", "material_id", materialID, "err", err)
		return
	}
	if !claimed {
		return
	}

	err = s.parseWithRetry(
		ctx,
		task.MaterialID,
		task.Generation,
		derefStr(task.Content),
		derefStr(task.FileType),
		derefStr(task.StorageKey),
	)
	status := "done"
	var parseError any
	var failureMessage string
	if err != nil {
		status = "failed"
		failureMessage = boundedParseError(err)
		parseError = failureMessage
	}
	dbCtx, cancel = context.WithTimeout(ctx, s.parsePolicy.dbTimeout)
	defer cancel()
	finished, updateErr := s.repos.FinishParseTask(
		dbCtx,
		materialID,
		task.Generation,
		status,
		parseError,
	)
	if updateErr != nil {
		slog.Error("update material parse status failed", "material_id", materialID, "err", updateErr)
	} else if !finished {
		slog.Info(
			"ignored stale material parse completion",
			"material_id", materialID,
			"generation", task.Generation,
		)
	}
	if err != nil && finished {
		slog.Error("material parse task exhausted retries", "material_id", materialID, "err", failureMessage)
		alertCtx, alertCancel := context.WithTimeout(context.WithoutCancel(ctx), s.parsePolicy.alertTimeout)
		defer alertCancel()
		if alertErr := s.parseAlerter.AlertParseFailure(alertCtx, materialID, failureMessage); alertErr != nil {
			slog.Error("send material parse failure alert", "material_id", materialID, "err", alertErr)
		}
	}
}

func (s *MaterialService) parseWithRetry(
	ctx context.Context,
	materialID int64,
	generation int64,
	content string,
	fileType string,
	storageKey string,
) error {
	var lastErr error
	for attempt := 1; attempt <= s.parsePolicy.maxAttempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, s.parsePolicy.timeout)
		lastErr = s.agent.Parse(
			attemptCtx,
			materialID,
			generation,
			content,
			fileType,
			storageKey,
		)
		cancel()
		if lastErr == nil {
			return nil
		}
		if attempt == s.parsePolicy.maxAttempts {
			break
		}
		delay := s.parsePolicy.baseDelay * time.Duration(1<<(attempt-1))
		slog.Warn("material parse attempt failed", "material_id", materialID, "attempt", attempt, "retry_in", delay, "err", boundedParseError(lastErr))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return fmt.Errorf("parse material %d canceled: %w", materialID, ctx.Err())
		case <-timer.C:
		}
	}
	return fmt.Errorf("parse material %d after %d attempts: %w", materialID, s.parsePolicy.maxAttempts, lastErr)
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
