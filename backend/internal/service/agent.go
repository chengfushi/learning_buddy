package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"learning_buddy/backend/internal/config"
	"learning_buddy/backend/internal/repository"
)

// #nosec G101 -- this is an HTTP header name, not a credential value.
const agentTokenHeader = "X-Agent-Token"

var errAgentSharedSecretMissing = errors.New("AGENT_SHARED_SECRET is not configured")

// AgentService 封装 Backend repository 检索与 Python Agent 生成调用。
// 安全边界：可见性与向量检索只发生在 repository，Agent 只接收已过滤 chunks。
type AgentService struct {
	cfg             *config.Config
	repos           *repository.Repositories
	client          *http.Client
	retrieveTimeout time.Duration
}

func NewAgentService(cfg *config.Config, repos *repository.Repositories) *AgentService {
	return &AgentService{
		cfg:             cfg,
		repos:           repos,
		retrieveTimeout: 2500 * time.Millisecond,
		client: &http.Client{
			Timeout: 120 * time.Second, // 解析/生成可能较慢，给足超时（呼应 R6 兜底由调用方处理）
		},
	}
}

// Chunk 检索片段。
type Chunk struct {
	TeamID     int64   `json:"team_id"`
	MaterialID int64   `json:"material_id"`
	Chapter    string  `json:"chapter"`
	ChunkIdx   int     `json:"chunk_idx"`
	Content    string  `json:"content"`
	ChunkID    int64   `json:"chunk_id,omitempty"`
	Title      string  `json:"title,omitempty"`
	Kind       string  `json:"kind,omitempty"`
	PageNumber *int    `json:"page_number,omitempty"`
	Score      float64 `json:"score,omitempty"`
	AssetID    *int64  `json:"asset_id,omitempty"`
}

type QueryAnalysisRequest struct {
	Question string        `json:"question"`
	History  []ChatHistory `json:"history"`
}

type QueryAnalysisResult struct {
	RetrievalQuery string    `json:"retrieval_query"`
	Keywords       []string  `json:"keywords"`
	Embedding      []float64 `json:"embedding"`
	RewriteApplied bool      `json:"rewrite_applied"`
	Model          string    `json:"model"`
}

type RerankCandidate struct {
	ChunkID    int64  `json:"chunk_id"`
	MaterialID int64  `json:"material_id"`
	Content    string `json:"content"`
	Title      string `json:"title"`
	Kind       string `json:"kind"`
}

type RerankRequest struct {
	Query      string            `json:"query"`
	Candidates []RerankCandidate `json:"candidates"`
	TopN       int               `json:"top_n"`
}

type RerankItem struct {
	ChunkID int64   `json:"chunk_id"`
	Score   float64 `json:"score"`
}

type RerankResult struct {
	Items    []RerankItem `json:"items"`
	Model    string       `json:"model"`
	Degraded bool         `json:"degraded"`
}

// PreparedContext 是完整检索阶段产物，用于生成、追踪与反馈闭环。
type PreparedContext struct {
	IndexVersion   string
	RetrievalQuery string
	RewriteApplied bool
	Chunks         []Chunk
	Candidates     []repository.RetrievedChunk
	StageMS        map[string]int64
	DegradedStages []string
}

// ParseRequest 触发解析的请求体。
type ParseRequest struct {
	MaterialID      int64  `json:"material_id"`
	ParseGeneration int64  `json:"parse_generation"`
	Content         string `json:"content"`
	FileType        string `json:"file_type"`
	StorageKey      string `json:"storage_key"`
}

// EmbedRequest Agent 向量化契约请求。
type EmbedRequest struct {
	Text string `json:"text"`
}

// Parse 触发 Agent 对资料做结构化解析（切分/嵌入/幂等写 chunks）。
func (s *AgentService) Parse(
	ctx context.Context,
	materialID int64,
	generation int64,
	content string,
	fileType string,
	storageKey string,
) error {
	req, err := newAgentRequest(ctx, s.cfg, "/parse", ParseRequest{
		MaterialID:      materialID,
		ParseGeneration: generation,
		Content:         content,
		FileType:        fileType,
		StorageKey:      storageKey,
	})
	if err != nil {
		return fmt.Errorf("create agent parse request: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("agent parse: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("agent parse failed: %s %s", resp.Status, string(b))
	}
	return nil
}

// RetrieveVisibleContext 先验证指定资料，再在 800ms 预算内向量化并调用 repository 检索。
// 向量化/检索失败时安全降级为空上下文；资料不可见则返回 ErrNotFound，不能降级绕过。
func (s *AgentService) RetrieveVisibleContext(
	ctx context.Context,
	userID int64,
	query string,
	materialID *int64,
	topK int,
) ([]Chunk, error) {
	prepared, err := s.PrepareVisibleContext(ctx, userID, query, nil, materialID)
	if err != nil {
		return nil, err
	}
	if topK > 0 && len(prepared.Chunks) > topK {
		return prepared.Chunks[:topK], nil
	}
	return prepared.Chunks, nil
}

// PrepareVisibleContext 在 2.5 秒总预算内执行 Query Rewrite、混合召回、Rerank 与父文档扩展。
func (s *AgentService) PrepareVisibleContext(
	ctx context.Context,
	userID int64,
	query string,
	history []ChatHistory,
	materialID *int64,
) (*PreparedContext, error) {
	retrieveCtx, cancel := context.WithTimeout(ctx, s.retrieveTimeout)
	defer cancel()
	prepared := &PreparedContext{
		RetrievalQuery: query,
		IndexVersion:   "unknown",
		StageMS:        make(map[string]int64, 4),
	}
	if materialID != nil {
		if err := s.repos.HasVisibleMaterial(retrieveCtx, userID, *materialID); err != nil {
			return nil, fmt.Errorf("authorize RAG material: %w", err)
		}
	}
	analyzeStarted := time.Now()
	analyzeCtx, analyzeCancel := context.WithTimeout(retrieveCtx, 1500*time.Millisecond)
	analysis, err := postJSON[QueryAnalysisResult](
		analyzeCtx,
		s.client,
		s.cfg,
		"/analyze-query",
		QueryAnalysisRequest{Question: query, History: history},
	)
	analyzeCancel()
	prepared.StageMS["analyze_query"] = time.Since(analyzeStarted).Milliseconds()
	if err != nil {
		slog.Warn("RAG query analysis degraded", "user_id", userID, "err", err)
		prepared.DegradedStages = append(prepared.DegradedStages, "query_analysis", "embedding")
		analysis = &QueryAnalysisResult{RetrievalQuery: query, Keywords: strings.Fields(query)}
		if retrieveCtx.Err() != nil {
			return prepared, nil
		}
	}
	prepared.RetrievalQuery = analysis.RetrievalQuery
	prepared.RewriteApplied = analysis.RewriteApplied
	lexicalTerms := append([]string{analysis.RetrievalQuery}, analysis.Keywords...)
	dbStarted := time.Now()
	dbCtx, dbCancel := context.WithTimeout(retrieveCtx, 300*time.Millisecond)
	retrieved, err := s.repos.HybridVisibleCandidates(
		dbCtx, userID, analysis.Embedding, lexicalTerms, materialID, 20,
	)
	dbCancel()
	prepared.StageMS["retrieve"] = time.Since(dbStarted).Milliseconds()
	if err != nil {
		slog.Warn("RAG retrieval degraded to empty context", "user_id", userID, "err", err)
		prepared.DegradedStages = append(prepared.DegradedStages, "retrieve")
		return prepared, nil
	}
	versions := make(map[string]bool)
	for _, item := range retrieved {
		versions[item.IndexVersion] = true
	}
	if len(versions) == 1 {
		for version := range versions {
			prepared.IndexVersion = version
		}
	} else if len(versions) > 1 {
		prepared.IndexVersion = "mixed"
	}
	if len(retrieved) > 0 {
		request := RerankRequest{Query: analysis.RetrievalQuery, TopN: 8}
		for _, item := range retrieved {
			request.Candidates = append(request.Candidates, RerankCandidate{
				ChunkID: item.ID, MaterialID: item.MaterialID, Content: item.Content,
				Title: item.Title, Kind: item.Kind,
			})
		}
		rerankStarted := time.Now()
		rerankCtx, rerankCancel := context.WithTimeout(retrieveCtx, 1200*time.Millisecond)
		reranked, rerankErr := postJSON[RerankResult](rerankCtx, s.client, s.cfg, "/rerank", request)
		rerankCancel()
		prepared.StageMS["rerank"] = time.Since(rerankStarted).Milliseconds()
		if rerankErr != nil {
			prepared.DegradedStages = append(prepared.DegradedStages, "rerank")
		} else {
			scores := make(map[int64]float64, len(reranked.Items))
			for _, item := range reranked.Items {
				scores[item.ChunkID] = item.Score
			}
			filtered := make([]repository.RetrievedChunk, 0, len(scores))
			for _, item := range retrieved {
				if score, ok := scores[item.ID]; ok {
					item.RerankScore = &score
					item.Score = score
					filtered = append(filtered, item)
				}
			}
			sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].Score > filtered[j].Score })
			if len(filtered) > 0 {
				retrieved = filtered
			}
			if reranked.Degraded {
				prepared.DegradedStages = append(prepared.DegradedStages, "rerank")
			}
		}
	}
	prepared.Candidates = retrieved
	expandStarted := time.Now()
	contextChunks, err := s.repos.ExpandVisibleContext(
		retrieveCtx, userID, retrieved, materialID, 3, 8, 12000,
	)
	prepared.StageMS["expand"] = time.Since(expandStarted).Milliseconds()
	if err != nil {
		return nil, fmt.Errorf("expand RAG context: %w", err)
	}
	prepared.Chunks = make([]Chunk, 0, len(contextChunks))
	for _, item := range contextChunks {
		prepared.Chunks = append(prepared.Chunks, Chunk{
			TeamID: item.TeamID, MaterialID: item.MaterialID, Chapter: item.Chapter,
			ChunkIdx: item.ChunkIdx, Content: item.Content, ChunkID: item.ID,
			Title: item.Title, Kind: item.Kind, PageNumber: item.PageNumber,
			Score: item.Score, AssetID: item.AssetID,
		})
	}
	return prepared, nil
}

// ChatRequest 答疑/规划/测评的统一请求体（SSE 流式）。
type ChatRequest struct {
	Question       string        `json:"question"`
	SessionID      string        `json:"session_id"`
	History        []ChatHistory `json:"history"`
	Chunks         []Chunk       `json:"chunks"`
	Service        string        `json:"service"` // chat/plan/quiz
	Deadline       string        `json:"deadline,omitempty"`
	Count          int           `json:"count,omitempty"`
	RetrievalQuery string        `json:"retrieval_query,omitempty"`
}

// ChatHistory 历史消息。
type ChatHistory struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// AgentStream 保存 Agent SSE 响应体及跨服务关联 ID。
type AgentStream struct {
	Body    io.ReadCloser
	TraceID string
}

// ChatStream 向 Agent 发起 SSE 流式答疑，返回底层流（由 handler 转发给前端）。
func (s *AgentService) ChatStream(ctx context.Context, req ChatRequest) (*AgentStream, error) {
	httpReq, err := newAgentRequest(ctx, s.cfg, "/chat", req)
	if err != nil {
		return nil, fmt.Errorf("create agent chat request: %w", err)
	}
	httpReq.Header.Set("Accept", "text/event-stream")
	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("agent chat: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("agent chat failed: %s %s", resp.Status, string(b))
	}
	return &AgentStream{
		Body:    resp.Body,
		TraceID: resp.Header.Get("X-Trace-ID"),
	}, nil
}

// PlanResult 学习计划结果。
type PlanResult struct {
	Title string          `json:"title"`
	Goal  string          `json:"goal"`
	Items []StudyPlanItem `json:"items"`
}

// StudyPlanItem 计划条目。
type StudyPlanItem struct {
	Date string `json:"date"`
	Task string `json:"task"`
	Done bool   `json:"done"`
}

// PlanRequest Agent 学习计划契约请求。
type PlanRequest struct {
	Goal     string  `json:"goal"`
	Deadline string  `json:"deadline"`
	Chunks   []Chunk `json:"chunks"`
}

// Plan 调用 Agent 生成学习计划（JSON）。
func (s *AgentService) Plan(ctx context.Context, userID int64, goal, deadline string) (*PlanResult, error) {
	chunks, err := s.RetrieveVisibleContext(ctx, userID, goal, nil, 5)
	if err != nil {
		return nil, fmt.Errorf("retrieve plan context: %w", err)
	}
	payload := PlanRequest{
		Goal:     goal,
		Deadline: deadline,
		Chunks:   chunks,
	}
	return postJSON[PlanResult](ctx, s.client, s.cfg, "/plan", payload)
}

// QuizItem 测评题目（Agent 生成）。
type QuizItem struct {
	Question   string   `json:"question"`
	Options    []string `json:"options"`
	AnswerKey  string   `json:"answer_key"`
	Difficulty string   `json:"difficulty"`
}

// QuizRequest Agent 测评契约请求。
type QuizRequest struct {
	Topic  string  `json:"topic"`
	Count  int     `json:"count"`
	Chunks []Chunk `json:"chunks"`
}

// Quiz 调用 Agent 生成测评题目（JSON）。
func (s *AgentService) Quiz(ctx context.Context, userID int64, topic string, materialID *int64, count int) ([]QuizItem, error) {
	chunks, err := s.RetrieveVisibleContext(ctx, userID, topic, materialID, count+2)
	if err != nil {
		return nil, fmt.Errorf("retrieve quiz context: %w", err)
	}
	payload := QuizRequest{
		Topic:  topic,
		Count:  count,
		Chunks: chunks,
	}
	res, err := postJSON[[]QuizItem](ctx, s.client, s.cfg, "/quiz", payload)
	if err != nil {
		return nil, err
	}
	return *res, nil
}

// postJSON 泛型辅助：携带服务凭证 POST JSON 并解码为 T。
func postJSON[T any](ctx context.Context, client *http.Client, cfg *config.Config, path string, payload any) (*T, error) {
	req, err := newAgentRequest(ctx, cfg, path, payload)
	if err != nil {
		return nil, fmt.Errorf("create agent %s request: %w", path, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("agent %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("agent %s failed: %s %s", path, resp.Status, string(b))
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode agent %s response: %w", path, err)
	}
	return &out, nil
}

func newAgentRequest(
	ctx context.Context,
	cfg *config.Config,
	path string,
	payload any,
) (*http.Request, error) {
	if cfg.AgentSharedSecret == "" {
		return nil, errAgentSharedSecretMissing
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request payload: %w", err)
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		cfg.AgentBaseURL+path,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(agentTokenHeader, cfg.AgentSharedSecret)
	return req, nil
}
