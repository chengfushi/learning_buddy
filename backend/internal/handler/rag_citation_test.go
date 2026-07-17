package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
