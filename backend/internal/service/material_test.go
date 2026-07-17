package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"learning_buddy/backend/internal/model"
	"learning_buddy/backend/internal/repository"
)

type fakeMaterialParser struct {
	mu          sync.Mutex
	failures    int
	attempts    int
	block       bool
	failureErr  error
	startedCh   chan struct{}
	generations []int64
	contents    []string
	onParse     func()
}

func (f *fakeMaterialParser) Parse(
	ctx context.Context,
	materialID int64,
	generation int64,
	content string,
	fileType string,
	storageKey string,
) error {
	if f.startedCh != nil {
		select {
		case f.startedCh <- struct{}{}:
		default:
		}
	}
	f.mu.Lock()
	f.attempts++
	f.generations = append(f.generations, generation)
	f.contents = append(f.contents, content)
	attempt := f.attempts
	block := f.block
	f.mu.Unlock()
	if f.onParse != nil {
		f.onParse()
	}
	if block {
		<-ctx.Done()
		return ctx.Err()
	}
	if attempt <= f.failures {
		if f.failureErr != nil {
			return f.failureErr
		}
		return errors.New("temporary agent failure")
	}
	return nil
}

type recordingParseAlerter struct {
	mu    sync.Mutex
	calls int
}

type recordingMaterialObjectStore struct {
	putCalls int
}

func (s *recordingMaterialObjectStore) PutSource(
	context.Context,
	int64,
	string,
	string,
	[]byte,
) (string, string, error) {
	s.putCalls++
	return "source/key.txt", "txt", nil
}

func (*recordingMaterialObjectStore) DeleteSource(context.Context, string) error { return nil }

func TestCreateWithFileAuthorizesBeforeObjectUpload(t *testing.T) {
	svcs, db := newTestServices(t)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	svcs.Repos.DB = tx

	owner := model.User{Email: "upload-owner-" + uuid.NewString() + "@test.dev", Role: "teacher"}
	require.NoError(t, tx.Create(&owner).Error)
	outsider := model.User{Email: "upload-outsider-" + uuid.NewString() + "@test.dev", Role: "teacher"}
	require.NoError(t, tx.Create(&outsider).Error)
	team := model.Team{Name: "upload-team", Type: "teacher", OwnerID: &owner.ID}
	require.NoError(t, tx.Create(&team).Error)
	objects := &recordingMaterialObjectStore{}
	svcs.Materials.objects = objects

	_, err := svcs.Materials.CreateWithFile(
		context.Background(),
		outsider.ID,
		outsider.Role,
		CreateInput{TeamID: team.ID, Title: "forbidden", OwnerID: outsider.ID},
		"guide.txt",
		"text/plain",
		strings.NewReader("content"),
	)
	assert.ErrorIs(t, err, ErrForbidden)
	assert.Zero(t, objects.putCalls, "unauthorized requests must not reach object storage")
}

func (a *recordingParseAlerter) AlertParseFailure(context.Context, int64, string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	return nil
}

func (a *recordingParseAlerter) callCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

func (f *fakeMaterialParser) lastPayload() (int64, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.generations) == 0 {
		return 0, ""
	}
	last := len(f.generations) - 1
	return f.generations[last], f.contents[last]
}

func (f *fakeMaterialParser) attemptCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attempts
}

func createParseTestMaterial(t *testing.T, tx *gorm.DB, status string) *model.Material {
	t.Helper()
	suffix := uuid.NewString()[:8]
	user := model.User{Email: "parse_" + suffix + "@test.dev", DisplayName: "parser", Role: "teacher"}
	require.NoError(t, tx.Create(&user).Error)
	team := model.Team{Name: "parse-" + suffix, Type: "teacher", OwnerID: &user.ID}
	require.NoError(t, tx.Create(&team).Error)
	content := "解析任务测试内容"
	material := model.Material{
		TeamID:          team.ID,
		Title:           "parse-test",
		Content:         &content,
		ParseStatus:     status,
		ParseGeneration: 1,
		OwnerID:         user.ID,
	}
	require.NoError(t, tx.Create(&material).Error)
	return &material
}

func createCommittedParseTestMaterial(t *testing.T, db *gorm.DB, status string) *model.Material {
	t.Helper()
	material := createParseTestMaterial(t, db, status)
	teamID := material.TeamID
	ownerID := material.OwnerID
	t.Cleanup(func() {
		require.NoError(t, db.Where("id = ?", material.ID).Delete(&model.Material{}).Error)
		require.NoError(t, db.Where("id = ?", teamID).Delete(&model.Team{}).Error)
		require.NoError(t, db.Where("id = ?", ownerID).Delete(&model.User{}).Error)
	})
	return material
}

func TestParseGenerationRejectsStaleCompletion(t *testing.T) {
	svcs, db := newTestServices(t)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	svcs.Repos.DB = tx

	material := createParseTestMaterial(t, tx, "pending")
	oldTask, claimed, err := svcs.Repos.ClaimParseTask(context.Background(), material.ID)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, tx.Model(&model.Material{}).
		Where("id = ?", material.ID).
		Updates(map[string]any{
			"parse_status":     "pending",
			"parse_generation": gorm.Expr("parse_generation + 1"),
		}).Error)
	newTask, claimed, err := svcs.Repos.ClaimParseTask(context.Background(), material.ID)
	require.NoError(t, err)
	require.True(t, claimed)
	require.Greater(t, newTask.Generation, oldTask.Generation)

	finished, err := svcs.Repos.FinishParseTask(
		context.Background(),
		material.ID,
		oldTask.Generation,
		"done",
		nil,
	)
	require.NoError(t, err)
	assert.False(t, finished, "旧 worker 不得完成新代次")

	parseError := "new generation failed"
	finished, err = svcs.Repos.FinishParseTask(
		context.Background(),
		material.ID,
		newTask.Generation,
		"failed",
		parseError,
	)
	require.NoError(t, err)
	require.True(t, finished)

	var got model.Material
	require.NoError(t, tx.Where("id = ?", material.ID).Limit(1).Find(&got).Error)
	assert.Equal(t, "failed", got.ParseStatus)
	require.NotNil(t, got.ParseError)
	assert.Equal(t, parseError, *got.ParseError)
}

func TestDelayedWorkerClaimsCanonicalContentForCurrentGeneration(t *testing.T) {
	svcs, db := newTestServices(t)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	svcs.Repos.DB = tx

	material := createParseTestMaterial(t, tx, "pending")
	newContent := "用户编辑后的最新正文"
	material, reparse, err := svcs.Repos.UpdateMaterial(
		context.Background(),
		material.ID,
		repository.MaterialUpdatePatch{Content: &newContent},
	)
	require.NoError(t, err)
	require.True(t, reparse)

	parser := &fakeMaterialParser{}
	svcs.Materials.agent = parser
	svcs.Materials.parsePolicy = testParsePolicy()
	svcs.Materials.triggerParse(context.Background(), material.ID)

	generation, content := parser.lastPayload()
	assert.Equal(t, material.ParseGeneration, generation)
	assert.Equal(t, newContent, content, "延迟启动的旧 goroutine 必须使用 claim 返回的当前正文")
}

func TestConcurrentMaterialUpdatesAtomicallyIncrementParseGeneration(t *testing.T) {
	svcs, db := newTestServices(t)
	material := createCommittedParseTestMaterial(t, db, "done")

	first := *material
	second := *material
	firstContent := "并发更新 A"
	secondContent := "并发更新 B"
	first.Content = &firstContent
	second.Content = &secondContent

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, candidate := range []*model.Material{&first, &second} {
		wg.Add(1)
		go func(item *model.Material) {
			defer wg.Done()
			<-start
			_, _, err := svcs.Repos.UpdateMaterial(
				context.Background(),
				item.ID,
				repository.MaterialUpdatePatch{Content: item.Content},
			)
			errs <- err
		}(candidate)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	got, err := svcs.Repos.GetMaterial(context.Background(), material.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(3), got.ParseGeneration)
	require.NotNil(t, got.Content)
	assert.Contains(t, []string{firstContent, secondContent}, *got.Content)
}

func TestConcurrentMaterialFieldPatchesDoNotOverwriteEachOther(t *testing.T) {
	svcs, db := newTestServices(t)
	material := createCommittedParseTestMaterial(t, db, "done")

	newContent := "并发后必须保留的新正文"
	newTitle := "并发后必须保留的新标题"
	shared := true
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, _, err := svcs.Repos.UpdateMaterial(
			context.Background(),
			material.ID,
			repository.MaterialUpdatePatch{Content: &newContent},
		)
		errs <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		_, _, err := svcs.Repos.UpdateMaterial(
			context.Background(),
			material.ID,
			repository.MaterialUpdatePatch{Title: &newTitle, Shared: &shared},
		)
		errs <- err
	}()
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	got, err := svcs.Repos.GetMaterial(context.Background(), material.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Content)
	assert.Equal(t, newContent, *got.Content)
	assert.Equal(t, newTitle, got.Title)
	assert.True(t, got.Shared)
	assert.Equal(t, int64(2), got.ParseGeneration, "只有正文变化应递增代次")
}

func testParsePolicy() parseRetryPolicy {
	return parseRetryPolicy{
		maxAttempts:  3,
		timeout:      100 * time.Millisecond,
		baseDelay:    time.Millisecond,
		dbTimeout:    time.Second,
		scanInterval: 10 * time.Millisecond,
		alertTimeout: time.Second,
		batchSize:    100,
	}
}

func TestTriggerParseRetriesThenSucceeds(t *testing.T) {
	svcs, db := newTestServices(t)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	svcs.Repos.DB = tx

	material := createParseTestMaterial(t, tx, "pending")
	parser := &fakeMaterialParser{failures: 2}
	svcs.Materials.agent = parser
	svcs.Materials.parsePolicy = testParsePolicy()

	svcs.Materials.triggerParse(context.Background(), material.ID)

	var got model.Material
	require.NoError(t, tx.Where("id = ?", material.ID).Limit(1).Find(&got).Error)
	assert.Equal(t, 3, parser.attemptCount())
	assert.Equal(t, "done", got.ParseStatus)
	assert.Nil(t, got.ParseError)
}

func TestTriggerParseExhaustionMovesToFailed(t *testing.T) {
	svcs, db := newTestServices(t)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	svcs.Repos.DB = tx

	material := createParseTestMaterial(t, tx, "pending")
	parser := &fakeMaterialParser{block: true}
	svcs.Materials.agent = parser
	policy := testParsePolicy()
	policy.timeout = 5 * time.Millisecond
	svcs.Materials.parsePolicy = policy

	svcs.Materials.triggerParse(context.Background(), material.ID)

	var got model.Material
	require.NoError(t, tx.Where("id = ?", material.ID).Limit(1).Find(&got).Error)
	assert.Equal(t, 3, parser.attemptCount())
	assert.Equal(t, "failed", got.ParseStatus)
	require.NotNil(t, got.ParseError)
	assert.Contains(t, *got.ParseError, "after 3 attempts")
	assert.Contains(t, *got.ParseError, "context deadline exceeded")
}

func TestTriggerParseFailurePersistsBoundedErrorAndSendsAlert(t *testing.T) {
	svcs, db := newTestServices(t)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	svcs.Repos.DB = tx

	alerts := make(chan parseFailureAlert, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var alert parseFailureAlert
		require.NoError(t, json.NewDecoder(r.Body).Decode(&alert))
		alerts <- alert
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	material := createParseTestMaterial(t, tx, "pending")
	longError := errors.New(strings.Repeat("解析失败", 200))
	parser := &fakeMaterialParser{failures: 1, failureErr: longError}
	svcs.Materials.agent = parser
	svcs.Materials.parseAlerter = newParseFailureAlerter(server.URL)
	policy := testParsePolicy()
	policy.maxAttempts = 1
	svcs.Materials.parsePolicy = policy

	svcs.Materials.triggerParse(context.Background(), material.ID)

	var got model.Material
	require.NoError(t, tx.Where("id = ?", material.ID).Limit(1).Find(&got).Error)
	assert.Equal(t, "failed", got.ParseStatus)
	require.NotNil(t, got.ParseError)
	assert.Len(t, []rune(*got.ParseError), maxParseErrorRunes)
	assert.True(t, strings.HasSuffix(*got.ParseError, "..."))

	select {
	case alert := <-alerts:
		assert.Equal(t, "material_parse_failed", alert.Event)
		assert.Equal(t, material.ID, alert.MaterialID)
		assert.Equal(t, *got.ParseError, alert.Error)
		assert.False(t, alert.OccurredAt.IsZero())
	case <-time.After(time.Second):
		t.Fatal("parse failure alert was not delivered")
	}
}

func TestStaleFailedParseDoesNotSendAlert(t *testing.T) {
	svcs, db := newTestServices(t)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	svcs.Repos.DB = tx

	material := createParseTestMaterial(t, tx, "pending")
	parser := &fakeMaterialParser{
		failures:   1,
		failureErr: errors.New("旧代次解析失败"),
		onParse: func() {
			require.NoError(t, tx.Model(&model.Material{}).
				Where("id = ?", material.ID).
				Updates(map[string]any{
					"parse_status":     "pending",
					"parse_generation": gorm.Expr("parse_generation + 1"),
				}).Error)
		},
	}
	alerter := &recordingParseAlerter{}
	svcs.Materials.agent = parser
	svcs.Materials.parseAlerter = alerter
	policy := testParsePolicy()
	policy.maxAttempts = 1
	svcs.Materials.parsePolicy = policy

	svcs.Materials.triggerParse(context.Background(), material.ID)

	var got model.Material
	require.NoError(t, tx.Where("id = ?", material.ID).Limit(1).Find(&got).Error)
	assert.Equal(t, "pending", got.ParseStatus)
	assert.Equal(t, int64(2), got.ParseGeneration)
	assert.Zero(t, alerter.callCount(), "陈旧 worker 的失败不应触发死信告警")
}

func TestRecoverParseTasksRequeuesInterruptedTask(t *testing.T) {
	svcs, db := newTestServices(t)

	material := createCommittedParseTestMaterial(t, db, "parsing")
	parser := &fakeMaterialParser{startedCh: make(chan struct{}, 1)}
	svcs.Materials.agent = parser
	svcs.Materials.parsePolicy = testParsePolicy()

	require.NoError(t, svcs.Materials.RecoverParseTasks(context.Background()))
	select {
	case <-parser.startedCh:
	case <-time.After(time.Second):
		t.Fatal("requeued parse task did not start")
	}
	require.Eventually(t, func() bool {
		var status string
		err := db.Model(&model.Material{}).
			Select("parse_status").
			Where("id = ?", material.ID).
			Scan(&status).Error
		return err == nil && status == "done"
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, 1, parser.attemptCount())
}

func TestRecoverParseTasksDispatchesPersistedPendingTask(t *testing.T) {
	svcs, db := newTestServices(t)

	material := createCommittedParseTestMaterial(t, db, "pending")
	parser := &fakeMaterialParser{startedCh: make(chan struct{}, 1)}
	svcs.Materials.agent = parser
	svcs.Materials.parsePolicy = testParsePolicy()

	require.NoError(t, svcs.Materials.RecoverParseTasks(context.Background()))
	select {
	case <-parser.startedCh:
	case <-time.After(time.Second):
		t.Fatal("persisted pending parse task did not start")
	}
	require.Eventually(t, func() bool {
		var status string
		err := db.Model(&model.Material{}).
			Select("parse_status").
			Where("id = ?", material.ID).
			Scan(&status).Error
		return err == nil && status == "done"
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, 1, parser.attemptCount())
}

func TestRunParseDispatcherFindsTaskCreatedAfterStartup(t *testing.T) {
	svcs, db := newTestServices(t)

	parser := &fakeMaterialParser{startedCh: make(chan struct{}, 1)}
	svcs.Materials.agent = parser
	svcs.Materials.parsePolicy = testParsePolicy()
	dispatcherCtx, cancel := context.WithCancel(context.Background())
	dispatcherDone := make(chan struct{})
	go func() {
		defer close(dispatcherDone)
		svcs.Materials.RunParseDispatcher(dispatcherCtx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-dispatcherDone:
		case <-time.After(time.Second):
			t.Error("parse dispatcher did not stop")
		}
	})

	material := createCommittedParseTestMaterial(t, db, "pending")
	select {
	case <-parser.startedCh:
	case <-time.After(time.Second):
		t.Fatal("periodic dispatcher did not start pending task")
	}
	require.Eventually(t, func() bool {
		var status string
		err := db.Model(&model.Material{}).
			Select("parse_status").
			Where("id = ?", material.ID).
			Scan(&status).Error
		return err == nil && status == "done"
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, 1, parser.attemptCount())
	cancel()
	select {
	case <-dispatcherDone:
	case <-time.After(time.Second):
		t.Fatal("parse dispatcher did not stop")
	}
}

func TestRetryParseRequeuesFailedMaterial(t *testing.T) {
	svcs, db := newTestServices(t)

	material := createCommittedParseTestMaterial(t, db, "failed")
	parseError := "previous failure"
	require.NoError(t, db.Model(material).Update("parse_error", parseError).Error)
	parser := &fakeMaterialParser{startedCh: make(chan struct{}, 1)}
	svcs.Materials.agent = parser
	svcs.Materials.parsePolicy = testParsePolicy()

	got, err := svcs.Materials.RetryParse(
		context.Background(),
		material.OwnerID,
		"teacher",
		material.ID,
	)
	require.NoError(t, err)
	assert.Equal(t, "pending", got.ParseStatus)
	assert.Nil(t, got.ParseError)
	select {
	case <-parser.startedCh:
	case <-time.After(time.Second):
		t.Fatal("retried parse task did not start")
	}
	require.Eventually(t, func() bool {
		var status string
		err := db.Model(&model.Material{}).
			Select("parse_status").
			Where("id = ?", material.ID).
			Scan(&status).Error
		return err == nil && status == "done"
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, 1, parser.attemptCount())
}

func TestRetryParseRejectsNonFailedMaterial(t *testing.T) {
	svcs, db := newTestServices(t)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	svcs.Repos.DB = tx

	material := createParseTestMaterial(t, tx, "done")
	parser := &fakeMaterialParser{}
	svcs.Materials.agent = parser
	svcs.Materials.parsePolicy = testParsePolicy()

	_, err := svcs.Materials.RetryParse(
		context.Background(),
		material.OwnerID,
		"teacher",
		material.ID,
	)
	assert.ErrorIs(t, err, ErrMaterialParseNotFailed)
	assert.Equal(t, 0, parser.attemptCount())
}

func TestRetryParseRejectsNonOwner(t *testing.T) {
	svcs, db := newTestServices(t)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	svcs.Repos.DB = tx

	material := createParseTestMaterial(t, tx, "failed")
	parser := &fakeMaterialParser{}
	svcs.Materials.agent = parser
	svcs.Materials.parsePolicy = testParsePolicy()

	_, err := svcs.Materials.RetryParse(
		context.Background(),
		material.OwnerID+1,
		"student",
		material.ID,
	)
	assert.ErrorIs(t, err, ErrForbidden)
	assert.Equal(t, 0, parser.attemptCount())
}
