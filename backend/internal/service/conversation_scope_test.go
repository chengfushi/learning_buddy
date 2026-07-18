package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"learning_buddy/backend/internal/model"
	"learning_buddy/backend/internal/repository"
)

func TestSameMaterialScope(t *testing.T) {
	first, same, other := int64(11), int64(11), int64(12)
	tests := []struct {
		name    string
		session *int64
		request *int64
		want    bool
	}{
		{name: "global", want: true},
		{name: "global cannot become material", request: &first, want: false},
		{name: "material cannot become global", session: &first, want: false},
		{name: "same material", session: &first, request: &same, want: true},
		{name: "different material", session: &first, request: &other, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, sameMaterialScope(test.session, test.request))
		})
	}
}

func TestConversationSessionScopeIsolation(t *testing.T) {
	svcs, db := newTestServices(t)
	ctx := context.Background()
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer func() { require.NoError(t, tx.Rollback().Error) }()
	svcs.Repos.DB = tx

	suffix := uuid.NewString()[:8]
	user, err := svcs.Auth.Register(
		ctx,
		"conversation_"+suffix+"@test.dev",
		"password123",
		"会话测试",
		"student",
	)
	require.NoError(t, err)
	var privateTeam model.Team
	result := tx.Where("owner_id = ? AND type = ?", user.ID, "private").Limit(1).Find(&privateTeam)
	require.NoError(t, result.Error)
	require.EqualValues(t, 1, result.RowsAffected)

	firstMaterial := model.Material{
		TeamID: privateTeam.ID, Title: "资料一", ParseStatus: "done", OwnerID: user.ID,
	}
	secondMaterial := model.Material{
		TeamID: privateTeam.ID, Title: "资料二", ParseStatus: "done", OwnerID: user.ID,
	}
	require.NoError(t, tx.Create(&firstMaterial).Error)
	require.NoError(t, tx.Create(&secondMaterial).Error)

	globalSession, err := svcs.Conversation.NewSession(ctx, user.ID, "全局会话", nil)
	require.NoError(t, err)
	materialSession, err := svcs.Conversation.NewSession(
		ctx,
		user.ID,
		"资料会话",
		&firstMaterial.ID,
	)
	require.NoError(t, err)
	_, err = svcs.Conversation.AppendMessage(
		ctx,
		materialSession,
		"user",
		"只属于资料一的问题",
		nil,
	)
	require.NoError(t, err)

	globalMessages, err := svcs.Conversation.MessagesForScope(ctx, globalSession, user.ID, nil)
	require.NoError(t, err)
	assert.Empty(t, globalMessages)
	materialMessages, err := svcs.Conversation.MessagesForScope(
		ctx,
		materialSession,
		user.ID,
		&firstMaterial.ID,
	)
	require.NoError(t, err)
	require.Len(t, materialMessages, 1)
	assert.Equal(t, "只属于资料一的问题", materialMessages[0].Content)

	_, err = svcs.Conversation.MessagesForScope(ctx, globalSession, user.ID, &firstMaterial.ID)
	assert.ErrorIs(t, err, repository.ErrNotFound)
	_, err = svcs.Conversation.MessagesForScope(ctx, materialSession, user.ID, nil)
	assert.ErrorIs(t, err, repository.ErrNotFound)
	_, err = svcs.Conversation.MessagesForScope(
		ctx,
		materialSession,
		user.ID,
		&secondMaterial.ID,
	)
	assert.ErrorIs(t, err, repository.ErrNotFound)
	_, err = svcs.Conversation.MessagesForScope(
		ctx,
		materialSession,
		user.ID+9999,
		&firstMaterial.ID,
	)
	assert.ErrorIs(t, err, repository.ErrNotFound)

	sessions, err := svcs.Conversation.ListSessions(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, sessions, 2)
	var foundMaterialSession bool
	for _, session := range sessions {
		if session.ID == materialSession {
			foundMaterialSession = true
			require.NotNil(t, session.MaterialID)
			assert.Equal(t, firstMaterial.ID, *session.MaterialID)
		}
	}
	assert.True(t, foundMaterialSession)
}
