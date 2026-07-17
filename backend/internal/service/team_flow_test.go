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
	"learning_buddy/backend/internal/model"
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
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	cfg := &config.Config{DBDSN: dsn, JWTSecret: "test", EmbeddingDim: 1024}
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

// TestMaterialSharedFlipTakesEffectImmediately 覆盖 R7：teacher 资料 shared 翻转后，
// 学生下一次基于 repository 真源的查询必须立即生效，且翻回 false 后不可残留可见。
func TestMaterialSharedFlipTakesEffectImmediately(t *testing.T) {
	svcs, db := newTestServices(t)
	ctx := context.Background()
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	svcs.Repos.DB = tx

	suffix := uuid.NewString()[:8]
	teacher, err := svcs.Auth.Register(ctx, "share_tea_"+suffix+"@t.dev", "password123", "老师", "teacher")
	require.NoError(t, err)
	student, err := svcs.Auth.Register(ctx, "share_stu_"+suffix+"@t.dev", "password123", "学生", "student")
	require.NoError(t, err)
	team, err := svcs.Teams.CreateTeacherTeam(ctx, teacher.ID, "共享测试"+suffix)
	require.NoError(t, err)
	require.NoError(t, tx.Create(&model.TeamMember{
		TeamID: team.ID,
		UserID: student.ID,
		Status: "approved",
	}).Error)
	material := model.Material{
		TeamID:      team.ID,
		Title:       "即时可见性测试",
		ParseStatus: "done",
		Shared:      false,
		OwnerID:     teacher.ID,
	}
	require.NoError(t, tx.Create(&material).Error)

	visibleTeamIDs, err := svcs.Teams.VisibleTeamIDs(ctx, student.ID)
	require.NoError(t, err)
	assert.Contains(t, visibleTeamIDs, team.ID)

	visibleMaterialIDs := func() []int64 {
		materials, listErr := svcs.Repos.ListVisibleMaterials(ctx, student.ID, visibleTeamIDs, nil, "", 100)
		require.NoError(t, listErr)
		ids := make([]int64, 0, len(materials))
		for i := range materials {
			ids = append(ids, materials[i].ID)
		}
		return ids
	}

	assert.NotContains(t, visibleMaterialIDs(), material.ID, "shared=false 草稿必须不可见")
	shared := true
	_, err = svcs.Materials.Update(ctx, teacher.ID, "teacher", material.ID, nil, nil, &shared)
	require.NoError(t, err)
	assert.Contains(t, visibleMaterialIDs(), material.ID, "shared=true 后下一次查询必须立即可见")

	shared = false
	_, err = svcs.Materials.Update(ctx, teacher.ID, "teacher", material.ID, nil, nil, &shared)
	require.NoError(t, err)
	assert.NotContains(t, visibleMaterialIDs(), material.ID, "翻回 shared=false 后必须立即不可见")
}
