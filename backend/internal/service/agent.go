package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"learning_buddy/backend/internal/config"
)

// AgentService 封装对 Python Agent 服务的 HTTP 调用（SSE 流式 / JSON）。
// 安全边界：后端向 Agent 下发「可见 team 集合 + shared 谓词标记」，Agent 仅持
// material_chunks 的检索能力，不直接访问权限表（见 docs/system-design.md §7.4）。
type AgentService struct {
	cfg    *config.Config
	client *http.Client
}

func NewAgentService(cfg *config.Config) *AgentService {
	return &AgentService{
		cfg: cfg,
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
	MaterialID int64  `json:"material_id"`
	Content    string `json:"content"`
	FileType   string `json:"file_type"`
	StorageKey string `json:"storage_key"`
}

// Parse 触发 Agent 对资料做结构化解析（切分/嵌入/写 chunks/回写 parse_status）。
func (s *AgentService) Parse(ctx context.Context, materialID int64, content, fileType, storageKey string) error {
	body, _ := json.Marshal(ParseRequest{
		MaterialID: materialID,
		Content:    content,
		FileType:   fileType,
		StorageKey: storageKey,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.AgentBaseURL+"/parse", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("agent parse: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("agent parse failed: %s %s", resp.Status, string(b))
	}
	return nil
}

// Retrieve 在可见 team 集合内做向量检索（供测试/调试）。
func (s *AgentService) Retrieve(ctx context.Context, query string, visibleTeamIDs []int64, topK int) ([]Chunk, error) {
	payload := map[string]interface{}{
		"query":                  query,
		"visible_team_ids":       visibleTeamIDs,
		"only_shared_in_teacher": true,
		"top_k":                  topK,
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.AgentBaseURL+"/retrieve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent retrieve: %s", resp.Status)
	}
	var out struct {
		Chunks []Chunk `json:"chunks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Chunks, nil
}

// ChatRequest 答疑/规划/测评的统一请求体（SSE 流式）。
type ChatRequest struct {
	Question       string        `json:"question"`
	SessionID      string        `json:"session_id"`
	History        []ChatHistory `json:"history"`
	VisibleTeamIDs []int64       `json:"visible_team_ids"`
	TopK           int           `json:"top_k"`
	Service        string        `json:"service"` // chat/plan/quiz
	MaterialID     *int64        `json:"material_id,omitempty"`
	Deadline       string        `json:"deadline,omitempty"`
	Count          int           `json:"count,omitempty"`
}

// ChatHistory 历史消息。
type ChatHistory struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatStream 向 Agent 发起 SSE 流式答疑，返回底层流（由 handler 转发给前端）。
func (s *AgentService) ChatStream(ctx context.Context, req ChatRequest) (io.ReadCloser, error) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.AgentBaseURL+"/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("agent chat: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("agent chat failed: %s %s", resp.Status, string(b))
	}
	return resp.Body, nil
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

// Plan 调用 Agent 生成学习计划（JSON）。
func (s *AgentService) Plan(ctx context.Context, goal, deadline string, visibleTeamIDs []int64) (*PlanResult, error) {
	payload := map[string]interface{}{
		"goal":             goal,
		"deadline":         deadline,
		"visible_team_ids": visibleTeamIDs,
	}
	return postJSON[PlanResult](ctx, s.client, s.cfg.AgentBaseURL, "/plan", payload)
}

// QuizItem 测评题目（Agent 生成）。
type QuizItem struct {
	Question   string   `json:"question"`
	Options    []string `json:"options"`
	AnswerKey  string   `json:"answer_key"`
	Difficulty string   `json:"difficulty"`
}

// Quiz 调用 Agent 生成测评题目（JSON）。
func (s *AgentService) Quiz(ctx context.Context, topic string, materialID *int64, count int, visibleTeamIDs []int64) ([]QuizItem, error) {
	payload := map[string]interface{}{
		"topic":            topic,
		"material_id":      materialID,
		"count":            count,
		"visible_team_ids": visibleTeamIDs,
	}
	res, err := postJSON[[]QuizItem](ctx, s.client, s.cfg.AgentBaseURL, "/quiz", payload)
	if err != nil {
		return nil, err
	}
	return *res, nil
}

// postJSON 泛型辅助：POST JSON 并解码为 T。
func postJSON[T any](ctx context.Context, client *http.Client, baseURL, path string, payload map[string]interface{}) (*T, error) {
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("agent %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("agent %s failed: %s %s", path, resp.Status, string(b))
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}
