package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"learning_buddy/backend/internal/model"
)

func openTestDB(t *testing.T) *gorm.DB {
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
	return db
}

// TestVisibleMaterialsScope_R2 是 R2（RAG 权限谓词被绕过）的核心集成测试：
// 学生在 teacher team 中，仅能看到 shared=true 的资料，绝不可看到 shared=false 的备课草稿。
// 数据在事务内创建并回滚，不污染业务库。
func TestVisibleMaterialsScope_R2(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()

	repo := New(tx)

	suffix := uuid.NewString()[:8]
	student := model.User{Email: "stu_" + suffix + "@t.dev", Role: "student"}
	require.NoError(t, tx.Create(&student).Error)
	teacher := model.User{Email: "tea_" + suffix + "@t.dev", Role: "teacher"}
	require.NoError(t, tx.Create(&teacher).Error)

	// teacher team（学习小组）
	team := model.Team{Name: "小组" + suffix, Type: "teacher", OwnerID: &teacher.ID}
	require.NoError(t, tx.Create(&team).Error)

	// 备课草稿（shared=false）——学生绝不可见
	draft := model.Material{TeamID: team.ID, Title: "草稿" + suffix, Shared: false, OwnerID: teacher.ID}
	require.NoError(t, tx.Create(&draft).Error)
	// 对学生公开（shared=true）
	open := model.Material{TeamID: team.ID, Title: "公开" + suffix, Shared: true, OwnerID: teacher.ID}
	require.NoError(t, tx.Create(&open).Error)

	// 批准学生加入
	require.NoError(t, tx.Create(&model.TeamMember{TeamID: team.ID, UserID: student.ID, Status: "approved"}).Error)

	// 计算学生可见 team 集合
	visible, err := repo.VisibleTeamIDs(ctx, student.ID)
	require.NoError(t, err)
	assert.Contains(t, visible, team.ID, "已审批加入的 teacher team 应在可见集合内")

	// 在可见集合内列出资料
	mats, err := repo.ListVisibleMaterials(ctx, student.ID, visible, nil, "", 100)
	require.NoError(t, err)

	gotIDs := make([]int64, 0, len(mats))
	for _, m := range mats {
		gotIDs = append(gotIDs, m.ID)
	}
	assert.NotContains(t, gotIDs, draft.ID, "R2 防护：学生绝不可召回 shared=false 的备课草稿")
	assert.Contains(t, gotIDs, open.ID, "对学生公开的资料应可见")

	// 反向：以 teacher（owner）身份查看自己团队，应同时看到草稿与公开
	tmats, err := repo.ListTeamMaterials(ctx, team.ID, teacher.ID)
	require.NoError(t, err)
	var tIDs []int64
	for _, m := range tmats {
		tIDs = append(tIDs, m.ID)
	}
	assert.Contains(t, tIDs, draft.ID, "老师本人应能看到自己的备课草稿")
	assert.Contains(t, tIDs, open.ID, "老师本人应能看到自己公开的资料")
}

// TestListTeamMaterials_OwnerVsStudent 验证单 team 视图同时强制成员关系与 shared 过滤。
func TestListTeamMaterials_OwnerVsStudent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	repo := New(tx)

	suffix := uuid.NewString()[:8]
	student := model.User{Email: "s2_" + suffix + "@t.dev", Role: "student"}
	require.NoError(t, tx.Create(&student).Error)
	teacher := model.User{Email: "t2_" + suffix + "@t.dev", Role: "teacher"}
	require.NoError(t, tx.Create(&teacher).Error)
	team := model.Team{Name: "小组" + suffix, Type: "teacher", OwnerID: &teacher.ID}
	require.NoError(t, tx.Create(&team).Error)
	draft := model.Material{TeamID: team.ID, Title: "草稿", Shared: false, OwnerID: teacher.ID}
	require.NoError(t, tx.Create(&draft).Error)
	open := model.Material{TeamID: team.ID, Title: "公开", Shared: true, OwnerID: teacher.ID}
	require.NoError(t, tx.Create(&open).Error)

	// 学生视角（未加入）绝不可仅凭 team ID 读取 shared 资料。
	stuMats, err := repo.ListTeamMaterials(ctx, team.ID, student.ID)
	require.NoError(t, err)
	assert.Empty(t, stuMats, "未加入的学生看不到 teacher team 的任何资料")

	// approved 成员仅可见 shared=true。
	require.NoError(t, tx.Create(&model.TeamMember{TeamID: team.ID, UserID: student.ID, Status: "approved"}).Error)
	stuMats, err = repo.ListTeamMaterials(ctx, team.ID, student.ID)
	require.NoError(t, err)
	require.Len(t, stuMats, 1)
	assert.Equal(t, open.ID, stuMats[0].ID)

	// 老师（owner）视角
	var dbgCnt int64
	tx.Model(&model.Material{}).Where("team_id = ?", team.ID).Count(&dbgCnt)
	t.Logf("DEBUG teacher.ID=%d team.ID=%d draft.ID=%d open.ID=%d cnt=%d", teacher.ID, team.ID, draft.ID, open.ID, dbgCnt)
	teaMats, err := repo.ListTeamMaterials(ctx, team.ID, teacher.ID)
	require.NoError(t, err)
	assert.Len(t, teaMats, 2, "老师本人能看到全部 2 条资料（含草稿）")

	// 详情查询与列表共用同一 repository scope。
	gotOpen, err := repo.GetVisibleMaterial(ctx, student.ID, open.ID)
	require.NoError(t, err)
	assert.Equal(t, open.ID, gotOpen.ID)
	_, err = repo.GetVisibleMaterial(ctx, student.ID, draft.ID)
	assert.ErrorIs(t, err, ErrNotFound)
	gotDraft, err := repo.GetVisibleMaterial(ctx, teacher.ID, draft.ID)
	require.NoError(t, err)
	assert.Equal(t, draft.ID, gotDraft.ID)
}

// TestRetrieveVisibleChunks_R2 验证所有 RAG 入口最终使用 repository 的统一可见性子查询。
func TestRetrieveVisibleChunks_R2(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	repo := New(tx)

	suffix := uuid.NewString()[:8]
	student := model.User{Email: "rag_stu_" + suffix + "@t.dev", Role: "student"}
	require.NoError(t, tx.Create(&student).Error)
	outsider := model.User{Email: "rag_out_" + suffix + "@t.dev", Role: "student"}
	require.NoError(t, tx.Create(&outsider).Error)
	teacher := model.User{Email: "rag_tea_" + suffix + "@t.dev", Role: "teacher"}
	require.NoError(t, tx.Create(&teacher).Error)
	team := model.Team{Name: "RAG" + suffix, Type: "teacher", OwnerID: &teacher.ID}
	require.NoError(t, tx.Create(&team).Error)
	draft := model.Material{TeamID: team.ID, Title: "秘密草稿", Shared: false, OwnerID: teacher.ID}
	require.NoError(t, tx.Create(&draft).Error)
	shared := model.Material{TeamID: team.ID, Title: "共享讲义", Shared: true, OwnerID: teacher.ID}
	require.NoError(t, tx.Create(&shared).Error)
	require.NoError(t, tx.Create(&model.TeamMember{
		TeamID: team.ID, UserID: student.ID, Status: "approved",
	}).Error)

	embedding := make([]float64, 1024)
	embedding[0] = 1
	for materialID, content := range map[int64]string{draft.ID: "秘密", shared.ID: "共享"} {
		require.NoError(t, tx.Exec(
			"INSERT INTO material_chunks (team_id, material_id, chunk_idx, content, embedding) "+
				"VALUES (?, ?, 0, ?, ?::vector)",
			team.ID, materialID, content, vectorLiteral(embedding),
		).Error)
	}

	studentChunks, err := repo.RetrieveVisibleChunks(ctx, student.ID, embedding, nil, 5)
	require.NoError(t, err)
	require.Len(t, studentChunks, 1)
	assert.Equal(t, shared.ID, studentChunks[0].MaterialID)

	outsiderChunks, err := repo.RetrieveVisibleChunks(ctx, outsider.ID, embedding, &shared.ID, 5)
	require.NoError(t, err)
	assert.Empty(t, outsiderChunks, "非成员不能通过指定 material_id 召回共享资料")

	_, err = repo.GetVisibleMaterial(ctx, outsider.ID, shared.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestHybridVisibleCandidatesMatchesChineseKeywordsIndependently(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	repo := New(tx)

	owner := model.User{Email: "lexical_" + uuid.NewString() + "@t.dev", Role: "student"}
	require.NoError(t, tx.Create(&owner).Error)
	team := model.Team{Name: "词法检索", Type: "private", OwnerID: &owner.ID}
	require.NoError(t, tx.Create(&team).Error)
	material := model.Material{
		TeamID: team.ID, Title: "消息队列手册", OwnerID: owner.ID, IndexVersion: "legacy-v1",
	}
	require.NoError(t, tx.Create(&material).Error)
	embedding := make([]float64, 1024)
	require.NoError(t, tx.Exec(
		"INSERT INTO material_chunks "+
			"(team_id, material_id, chunk_idx, content, lexical_text, embedding, index_version) "+
			"VALUES (?, ?, 0, ?, ?, ?::vector, 'legacy-v1')",
		team.ID,
		material.ID,
		"消息队列连接配置参数",
		"消息队列连接配置参数",
		vectorLiteral(embedding),
	).Error)

	chunks, err := repo.HybridVisibleCandidates(
		ctx,
		owner.ID,
		nil,
		[]string{"它怎么配置", "消息队列", "配置"},
		nil,
		20,
	)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	assert.Equal(t, material.ID, chunks[0].MaterialID)
}
