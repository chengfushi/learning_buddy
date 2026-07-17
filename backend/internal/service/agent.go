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
		retrieveTimeout: 800 * time.Millisecond,
		client: &http.Client{
			Timeout: 120 * time.Second, // 解析/生成可能较慢，给足超时（呼应 R6 兜底由调用方处理）
		},
	}
}

// Chunk 检索片段。
type Chunk struct {
	TeamID     int64  `json:"team_id"`
	MaterialID int64  `json:"material_id"`
	Chapter    string `json:"chapter"`
	ChunkIdx   int    `json:"chunk_idx"`
	Content    string `json:"content"`
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
	retrieveCtx, cancel := context.WithTimeout(ctx, s.retrieveTimeout)
	defer cancel()
	if materialID != nil {
		if err := s.repos.HasVisibleMaterial(retrieveCtx, userID, *materialID); err != nil {
			return nil, fmt.Errorf("authorize RAG material: %w", err)
		}
	}
	result, err := postJSON[struct {
		Embedding []float64 `json:"embedding"`
	}](retrieveCtx, s.client, s.cfg, "/embed", EmbedRequest{Text: query})
	if err != nil {
		slog.Warn("RAG embedding degraded to empty context", "user_id", userID, "err", err)
		return []Chunk{}, nil
	}

	retrieved, err := s.repos.RetrieveVisibleChunks(retrieveCtx, userID, result.Embedding, materialID, topK)
	if err != nil {
		slog.Warn("RAG retrieval degraded to empty context", "user_id", userID, "err", err)
		return []Chunk{}, nil
	}
	chunks := make([]Chunk, 0, len(retrieved))
	for _, item := range retrieved {
		chunks = append(chunks, Chunk{
			TeamID: item.TeamID, MaterialID: item.MaterialID, Chapter: item.Chapter,
			ChunkIdx: item.ChunkIdx, Content: item.Content,
		})
	}
	return chunks, nil
}

// ChatRequest 答疑/规划/测评的统一请求体（SSE 流式）。
type ChatRequest struct {
	Question  string        `json:"question"`
	SessionID string        `json:"session_id"`
	History   []ChatHistory `json:"history"`
	Chunks    []Chunk       `json:"chunks"`
	Service   string        `json:"service"` // chat/plan/quiz
	Deadline  string        `json:"deadline,omitempty"`
	Count     int           `json:"count,omitempty"`
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
