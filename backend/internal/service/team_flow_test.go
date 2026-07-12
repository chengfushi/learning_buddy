package service

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"learning_buddy/backend/internal/config"
	"learning_buddy/backend/internal/repository"
)

func newTestServices(t *testing.T) (*Services, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/learning_buddy?sslmode=disable"
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	cfg := &config.Config{DBDSN: dsn, JWTSecret: "test", EmbeddingDim: 768}
	repos := repository.New(db)
	return New(repos, cfg), db
}

// TestTeamApprovalFlow 覆盖 F2.1/F2.5：老师建组 → 学生凭码加入(pending) →
// 审批前不可见、审批后可见。数据在事务内创建并回滚。
func TestTeamApprovalFlow(t *testing.T) {
	svcs, db := newTestServices(t)
	ctx := context.Background()
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	svcs.Repos.DB = tx

	suffix := uuid.NewString()[:8]
	teacher, err := svcs.Auth.Register(ctx, "tea_"+suffix+"@t.dev", "password123", "老师", "teacher")
	require.NoError(t, err)
	student, err := svcs.Auth.Register(ctx, "stu_"+suffix+"@t.dev", "password123", "学生", "student")
	require.NoError(t, err)

	// 老师建组 → 返回 join_code
	team, err := svcs.Teams.CreateTeacherTeam(ctx, teacher.ID, "物理小组"+suffix)
	require.NoError(t, err)
	require.NotNil(t, team.JoinCode)

	// 学生凭码加入 → pending
	m, err := svcs.Teams.JoinByCode(ctx, student.ID, *team.JoinCode)
	require.NoError(t, err)
	assert.Equal(t, "pending", m.Status)

	// 审批前：学生可见集合不应包含该 teacher team
	before, err := svcs.Teams.VisibleTeamIDs(ctx, student.ID)
	require.NoError(t, err)
	assert.NotContains(t, before, team.ID, "审批前学生不可见该小组资料")

	// 老师审批
	err = svcs.Teams.ApproveMember(ctx, team.ID, teacher.ID, student.ID)
	require.NoError(t, err)

	// 审批后：学生可见集合应包含该 teacher team
	after, err := svcs.Teams.VisibleTeamIDs(ctx, student.ID)
	require.NoError(t, err)
	assert.Contains(t, after, team.ID, "审批后学生可见该小组资料")

	// 非 owner 不能审批
	err = svcs.Teams.ApproveMember(ctx, team.ID, student.ID, student.ID)
	assert.Error(t, err, "非 owner 审批应被拒绝")
}
