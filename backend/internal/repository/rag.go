package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const maxRAGTopK = 50

// RetrievedChunk 是 repository 完成可见性过滤后返回给业务层的检索结果。
type RetrievedChunk struct {
	TeamID     int64   `gorm:"column:team_id"`
	MaterialID int64   `gorm:"column:material_id"`
	Chapter    string  `gorm:"column:chapter"`
	ChunkIdx   int     `gorm:"column:chunk_idx"`
	Content    string  `gorm:"column:content"`
	Score      float64 `gorm:"column:score"`
}

// RetrieveVisibleChunks 在用户可见资料子查询上执行向量检索。
// materialID 非空时仍先应用统一可见性谓词，避免通过测评/答疑的资料 ID 绕过 R2。
func (r *Repositories) RetrieveVisibleChunks(
	ctx context.Context,
	userID int64,
	embedding []float64,
	materialID *int64,
	topK int,
) ([]RetrievedChunk, error) {
	if len(embedding) == 0 {
		return nil, errors.New("retrieve visible chunks: empty embedding")
	}
	if topK <= 0 {
		topK = 5
	}
	if topK > maxRAGTopK {
		topK = maxRAGTopK
	}

	teamIDs, err := r.VisibleTeamIDs(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("retrieve visible chunks team ids: %w", err)
	}
	if len(teamIDs) == 0 {
		return []RetrievedChunk{}, nil
	}

	visibleMaterials := r.DB.WithContext(ctx).
		Table("materials").
		Select("materials.id").
		Scopes(r.VisibleMaterialsScope(userID, teamIDs))
	if materialID != nil {
		visibleMaterials = visibleMaterials.Where("materials.id = ?", *materialID)
	}

	vector := vectorLiteral(embedding)
	var chunks []RetrievedChunk
	result := r.DB.WithContext(ctx).
		Table("material_chunks AS c").
		Select(
			"c.team_id, c.material_id, COALESCE(m.chapter, '') AS chapter, "+
				"c.chunk_idx, c.content, 1 - (c.embedding <=> ?::vector) AS score",
			vector,
		).
		Joins("JOIN materials AS m ON m.id = c.material_id").
		Joins("JOIN (?) AS visible_materials ON visible_materials.id = c.material_id", visibleMaterials).
		Order("score DESC").
		Limit(topK).
		Scan(&chunks)
	if result.Error != nil {
		return nil, fmt.Errorf("retrieve visible chunks: %w", result.Error)
	}
	return chunks, nil
}

// HasVisibleMaterial 用统一可见性谓词判断资料是否存在于用户可见范围。
func (r *Repositories) HasVisibleMaterial(ctx context.Context, userID, materialID int64) error {
	_, err := r.GetVisibleMaterial(ctx, userID, materialID)
	if err != nil {
		return fmt.Errorf("check visible material: %w", err)
	}
	return nil
}

func vectorLiteral(values []float64) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, value := range values {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(value, 'g', -1, 64))
	}
	b.WriteByte(']')
	return b.String()
}
