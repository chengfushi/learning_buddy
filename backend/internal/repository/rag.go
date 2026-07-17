// package repository —— RAG 检索；所有候选与父文档扩展都复用统一资料可见性谓词。
package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

const (
	maxRAGTopK  = 50
	rrfConstant = 60.0
	vectorTopK  = 30
	lexicalTopK = 30
	defaultTopK = 20
)

// RetrievedChunk 是 repository 完成可见性过滤后返回给业务层的候选与上下文。
type RetrievedChunk struct {
	ID           int64    `gorm:"column:id"`
	TeamID       int64    `gorm:"column:team_id"`
	MaterialID   int64    `gorm:"column:material_id"`
	IndexVersion string   `gorm:"column:index_version"`
	Title        string   `gorm:"column:title"`
	Chapter      string   `gorm:"column:chapter"`
	ChunkIdx     int      `gorm:"column:chunk_idx"`
	Kind         string   `gorm:"column:kind"`
	HeadingPath  string   `gorm:"column:heading_path"`
	Content      string   `gorm:"column:content"`
	PageNumber   *int     `gorm:"column:page_number"`
	TokenCount   int      `gorm:"column:token_count"`
	AssetID      *int64   `gorm:"column:asset_id"`
	Score        float64  `gorm:"column:score"`
	VectorScore  *float64 `gorm:"-"`
	LexicalScore *float64 `gorm:"-"`
	RRFScore     float64  `gorm:"-"`
	RerankScore  *float64 `gorm:"-"`
}

func (r *Repositories) visibleRAGMaterials(
	ctx context.Context,
	userID int64,
	materialID *int64,
) (*gorm.DB, error) {
	teamIDs, err := r.VisibleTeamIDs(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("retrieve visible chunks team ids: %w", err)
	}
	if len(teamIDs) == 0 {
		return nil, nil
	}
	query := r.DB.WithContext(ctx).
		Table("materials").
		Select("materials.id, materials.index_version").
		Scopes(r.VisibleMaterialsScope(userID, teamIDs))
	if materialID != nil {
		query = query.Where("materials.id = ?", *materialID)
	}
	return query, nil
}

func candidateSelect(scoreExpression string) string {
	return "c.id, c.team_id, c.material_id, c.index_version, m.title, COALESCE(m.chapter, '') AS chapter, " +
		"c.chunk_idx, c.kind, COALESCE(c.heading_path, '') AS heading_path, c.content, c.page_number, c.token_count, c.asset_id, " +
		scoreExpression + " AS score"
}

// HybridVisibleCandidates 在同一权限子查询内执行向量与词法召回，并以 RRF 融合。
func (r *Repositories) HybridVisibleCandidates(
	ctx context.Context,
	userID int64,
	embedding []float64,
	lexicalTerms []string,
	materialID *int64,
	limit int,
) ([]RetrievedChunk, error) {
	if limit <= 0 {
		limit = defaultTopK
	}
	if limit > maxRAGTopK {
		limit = maxRAGTopK
	}
	visibleMaterials, err := r.visibleRAGMaterials(ctx, userID, materialID)
	if err != nil || visibleMaterials == nil {
		return []RetrievedChunk{}, err
	}

	var vectorRows []RetrievedChunk
	if len(embedding) > 0 {
		vector := vectorLiteral(embedding)
		err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("SET LOCAL hnsw.ef_search = 100").Error; err != nil {
				return fmt.Errorf("set hnsw ef_search: %w", err)
			}
			var vectorVersion string
			if err := tx.Raw("SELECT extversion FROM pg_extension WHERE extname = 'vector'").Scan(&vectorVersion).Error; err != nil {
				return fmt.Errorf("read pgvector version: %w", err)
			}
			if pgvectorSupportsIterativeScan(vectorVersion) {
				if err := tx.Exec("SET LOCAL hnsw.iterative_scan = strict_order").Error; err != nil {
					return fmt.Errorf("set hnsw iterative scan: %w", err)
				}
			}
			result := tx.Table("material_chunks AS c").
				Select(candidateSelect("1 - (c.embedding <=> ?::vector)"), vector).
				Joins("JOIN materials AS m ON m.id = c.material_id").
				Joins(
					"JOIN (?) AS visible_materials ON visible_materials.id = c.material_id "+
						"AND visible_materials.index_version = c.index_version",
					visibleMaterials,
				).
				Order("score DESC").
				Limit(vectorTopK).
				Scan(&vectorRows)
			if result.Error != nil {
				return fmt.Errorf("vector retrieve visible chunks: %w", result.Error)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	var lexicalRows []RetrievedChunk
	lexicalTerms = normalizeLexicalTerms(lexicalTerms)
	if len(lexicalTerms) > 0 {
		tsQuery := strings.Join(lexicalTerms, " OR ")
		scoreParts := []string{"ts_rank_cd(c.lexical_tsv, websearch_to_tsquery('simple', ?))"}
		scoreArgs := []any{tsQuery}
		whereParts := []string{"c.lexical_tsv @@ websearch_to_tsquery('simple', ?)"}
		whereArgs := []any{tsQuery}
		for _, term := range lexicalTerms {
			scoreParts = append(scoreParts, "similarity(c.lexical_text, ?)")
			scoreArgs = append(scoreArgs, term)
			whereParts = append(whereParts, "c.lexical_text % ?", "c.lexical_text ILIKE ?")
			whereArgs = append(whereArgs, term, "%"+term+"%")
		}
		scoreExpression := "GREATEST(" + strings.Join(scoreParts, ", ") + ")"
		result := r.DB.WithContext(ctx).
			Table("material_chunks AS c").
			Select(candidateSelect(scoreExpression), scoreArgs...).
			Joins("JOIN materials AS m ON m.id = c.material_id").
			Joins(
				"JOIN (?) AS visible_materials ON visible_materials.id = c.material_id "+
					"AND visible_materials.index_version = c.index_version",
				visibleMaterials,
			).
			Where(strings.Join(whereParts, " OR "), whereArgs...).
			Order("score DESC").
			Limit(lexicalTopK).
			Scan(&lexicalRows)
		if result.Error != nil {
			return nil, fmt.Errorf("lexical retrieve visible chunks: %w", result.Error)
		}
	}

	byID := make(map[int64]*RetrievedChunk, len(vectorRows)+len(lexicalRows))
	for rank := range vectorRows {
		row := vectorRows[rank]
		score := row.Score
		row.VectorScore = &score
		row.RRFScore = 1 / (rrfConstant + float64(rank+1))
		byID[row.ID] = &row
	}
	for rank := range lexicalRows {
		row := lexicalRows[rank]
		score := row.Score
		if existing, ok := byID[row.ID]; ok {
			existing.LexicalScore = &score
			existing.RRFScore += 1 / (rrfConstant + float64(rank+1))
			continue
		}
		row.LexicalScore = &score
		row.RRFScore = 1 / (rrfConstant + float64(rank+1))
		byID[row.ID] = &row
	}
	merged := make([]RetrievedChunk, 0, len(byID))
	for _, row := range byID {
		row.Score = row.RRFScore
		merged = append(merged, *row)
	}
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].RRFScore > merged[j].RRFScore })
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

func normalizeLexicalTerms(terms []string) []string {
	const maxTerms = 16
	result := make([]string, 0, min(len(terms), maxTerms))
	seen := make(map[string]struct{}, len(terms))
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		key := strings.ToLower(term)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, term)
		if len(result) == maxTerms {
			break
		}
	}
	return result
}

func pgvectorSupportsIterativeScan(version string) bool {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	return majorErr == nil && minorErr == nil && (major > 0 || minor >= 8)
}

// ExpandVisibleContext 对已排序候选做父资料扩展，并再次套用完整可见性谓词。
func (r *Repositories) ExpandVisibleContext(
	ctx context.Context,
	userID int64,
	ranked []RetrievedChunk,
	materialID *int64,
	maxMaterials int,
	maxChunks int,
	maxTokens int,
) ([]RetrievedChunk, error) {
	if len(ranked) == 0 {
		return []RetrievedChunk{}, nil
	}
	materialIDs := make([]int64, 0, maxMaterials)
	best := make(map[int64]RetrievedChunk)
	for _, candidate := range ranked {
		if _, exists := best[candidate.MaterialID]; exists {
			continue
		}
		best[candidate.MaterialID] = candidate
		materialIDs = append(materialIDs, candidate.MaterialID)
		if len(materialIDs) == maxMaterials {
			break
		}
	}
	visibleMaterials, err := r.visibleRAGMaterials(ctx, userID, materialID)
	if err != nil || visibleMaterials == nil {
		return []RetrievedChunk{}, err
	}
	var bodies []RetrievedChunk
	result := r.DB.WithContext(ctx).
		Table("material_chunks AS c").
		Select(candidateSelect("0")).
		Joins("JOIN materials AS m ON m.id = c.material_id").
		Joins(
			"JOIN (?) AS visible_materials ON visible_materials.id = c.material_id "+
				"AND visible_materials.index_version = c.index_version",
			visibleMaterials,
		).
		Where("c.material_id IN ? AND c.kind = ?", materialIDs, "body").
		Order("c.material_id, c.chunk_idx").
		Scan(&bodies)
	if result.Error != nil {
		return nil, fmt.Errorf("expand visible parent chunks: %w", result.Error)
	}
	grouped := make(map[int64][]RetrievedChunk)
	for _, body := range bodies {
		grouped[body.MaterialID] = append(grouped[body.MaterialID], body)
	}
	selected := make([]RetrievedChunk, 0, maxChunks)
	tokens := 0
	for _, id := range materialIDs {
		chunks := grouped[id]
		if len(chunks) == 0 {
			continue
		}
		hit := best[id]
		bestIndex := hit.ChunkIdx
		bestHeading := hit.HeadingPath
		semanticParentHit := hit.Kind != "body"
		assetAttached := false
		if hit.Kind != "body" {
			bestIndex = 0
		}
		for _, chunk := range chunks {
			sameHeading := bestHeading != "" && chunk.HeadingPath == bestHeading
			adjacent := chunk.ChunkIdx >= bestIndex-1 && chunk.ChunkIdx <= bestIndex+1
			if len(chunks) > 1 && !semanticParentHit && !sameHeading && !adjacent {
				continue
			}
			attachedThisChunk := false
			if hit.Kind == "image" && hit.AssetID != nil && !assetAttached &&
				(hit.PageNumber == nil || chunk.PageNumber == nil || *hit.PageNumber == *chunk.PageNumber) {
				chunk.Content += "\n\n[图片说明]\n" + hit.Content
				chunk.AssetID = hit.AssetID
				if chunk.PageNumber == nil {
					chunk.PageNumber = hit.PageNumber
				}
				chunk.TokenCount += hit.TokenCount
				attachedThisChunk = true
			}
			if len(selected) >= maxChunks || tokens+chunk.TokenCount > maxTokens {
				continue
			}
			chunk.Score = hit.Score
			chunk.RRFScore = hit.RRFScore
			chunk.RerankScore = hit.RerankScore
			selected = append(selected, chunk)
			tokens += chunk.TokenCount
			assetAttached = assetAttached || attachedThisChunk
		}
	}
	return selected, nil
}

// RetrieveVisibleChunks 保持旧调用兼容，内部使用新的权限安全候选查询。
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
	return r.HybridVisibleCandidates(ctx, userID, embedding, nil, materialID, topK)
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
