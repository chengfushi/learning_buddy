package handler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"learning_buddy/backend/internal/model"
	"learning_buddy/backend/internal/service"
)

func TestValidateCitationsRebuildsAuthorizedFieldsAndDropsUnknownIDs(t *testing.T) {
	chunkID := int64(9)
	chunks := []service.Chunk{{
		ChunkID: chunkID, TeamID: 3, MaterialID: 4, Title: "可信标题",
		Chapter: "第一章", ChunkIdx: 2, Content: "可信正文", Kind: "body", Score: .8,
	}}
	raw := []service.Citation{
		{ChunkID: &chunkID, MaterialID: 4, Title: "模型伪造标题", Snippet: "模型伪造正文"},
		{ChunkID: int64Pointer(10), MaterialID: 999},
	}
	validated := validateCitations(raw, chunks)
	require.Len(t, validated, 1)
	assert.Equal(t, "可信标题", validated[0].Title)
	assert.Equal(t, "可信正文", validated[0].Snippet)
	assert.Equal(t, int64(4), validated[0].MaterialID)
}

func int64Pointer(value int64) *int64 { return &value }

func TestSessionMessageViewsDecodeCitationsAndTolerateCorruptHistory(t *testing.T) {
	createdAt := time.Date(2026, 7, 18, 7, 0, 0, 0, time.UTC)
	citations, err := json.Marshal([]service.Citation{{
		TeamID: 1, MaterialID: 2, Chapter: "第一章", ChunkIdx: 3,
	}})
	require.NoError(t, err)

	views := sessionMessageViews([]model.AgentMessage{
		{ID: 10, SessionID: "session-1", Role: "assistant", Content: "回答", Citations: citations, CreatedAt: createdAt},
		{ID: 11, SessionID: "session-1", Role: "assistant", Content: "旧回答", Citations: []byte("broken")},
		{ID: 12, SessionID: "session-1", Role: "assistant", Content: "空引用", Citations: []byte("null")},
	})

	require.Len(t, views, 3)
	require.Len(t, views[0].Citations, 1)
	assert.Equal(t, int64(2), views[0].Citations[0].MaterialID)
	assert.Equal(t, createdAt, views[0].CreatedAt)
	assert.Empty(t, views[1].Citations)
	assert.NotNil(t, views[2].Citations)
	assert.Empty(t, views[2].Citations)
}
