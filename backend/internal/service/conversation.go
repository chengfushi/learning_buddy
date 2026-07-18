package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"learning_buddy/backend/internal/model"
	"learning_buddy/backend/internal/repository"
)

// ConversationService 对话会话与历史（F4/F5）。
type ConversationService struct {
	repos *repository.Repositories
}

// NewConversationService 由 service.New 间接构造（为保持 Services 扁平，这里单独提供工厂）。
func NewConversationService(repos *repository.Repositories) *ConversationService {
	return &ConversationService{repos: repos}
}

// NewSession 创建新会话，返回 session id（UUID）。
func (s *ConversationService) NewSession(
	ctx context.Context,
	userID int64,
	title string,
	materialID *int64,
) (string, error) {
	id := uuid.NewString()
	t := title
	sess := &model.AgentSession{ID: id, UserID: userID, MaterialID: materialID, Title: &t}
	if err := s.repos.DB.WithContext(ctx).Create(sess).Error; err != nil {
		return "", fmt.Errorf("create conversation session: %w", err)
	}
	return id, nil
}

// ListSessions 我的会话列表（按时间倒序）。
func (s *ConversationService) ListSessions(ctx context.Context, userID int64) ([]model.AgentSession, error) {
	var items []model.AgentSession
	if err := s.repos.DB.WithContext(ctx).Where("user_id = ?", userID).Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list conversation sessions: %w", err)
	}
	return items, nil
}

// GetSession 取会话元信息。
func (s *ConversationService) GetSession(ctx context.Context, sessionID string, userID int64) (*model.AgentSession, error) {
	var sess model.AgentSession
	result := s.repos.DB.WithContext(ctx).
		Where("id = ? AND user_id = ?", sessionID, userID).
		Limit(1).
		Find(&sess)
	if result.Error != nil {
		return nil, fmt.Errorf("get conversation session: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, repository.ErrNotFound
	}
	return &sess, nil
}

// Messages 取会话消息（历史，按时间正序）。
func (s *ConversationService) Messages(ctx context.Context, sessionID string, userID int64) ([]model.AgentMessage, error) {
	if _, err := s.GetSession(ctx, sessionID, userID); err != nil {
		return nil, err
	}
	var msgs []model.AgentMessage
	if err := s.repos.DB.WithContext(ctx).Where("session_id = ?", sessionID).Order("created_at ASC").Find(&msgs).Error; err != nil {
		return nil, fmt.Errorf("list conversation messages: %w", err)
	}
	return msgs, nil
}

// MessagesForScope 仅在会话所属资料与本次请求完全一致时返回历史。
// nil 表示全局会话；不一致统一返回 ErrNotFound，避免跨作用域污染上下文。
func (s *ConversationService) MessagesForScope(
	ctx context.Context,
	sessionID string,
	userID int64,
	materialID *int64,
) ([]model.AgentMessage, error) {
	session, err := s.GetSession(ctx, sessionID, userID)
	if err != nil {
		return nil, err
	}
	if !sameMaterialScope(session.MaterialID, materialID) {
		return nil, repository.ErrNotFound
	}
	var messages []model.AgentMessage
	if err := s.repos.DB.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("created_at ASC").
		Find(&messages).Error; err != nil {
		return nil, fmt.Errorf("list scoped conversation messages: %w", err)
	}
	return messages, nil
}

func sameMaterialScope(sessionMaterialID, requestMaterialID *int64) bool {
	if sessionMaterialID == nil || requestMaterialID == nil {
		return sessionMaterialID == nil && requestMaterialID == nil
	}
	return *sessionMaterialID == *requestMaterialID
}

// AppendMessage 写一条消息（user/assistant/system）。
func (s *ConversationService) AppendMessage(ctx context.Context, sessionID, role, content string, citations []Citation) (*model.AgentMessage, error) {
	m := model.AgentMessage{SessionID: sessionID, Role: role, Content: content}
	if citations != nil {
		b, _ := json.Marshal(citations)
		m.Citations = b
	}
	if err := s.repos.DB.WithContext(ctx).Create(&m).Error; err != nil {
		return nil, fmt.Errorf("append conversation message: %w", err)
	}
	return &m, nil
}

// Citation 引用来源（对应 agent_messages.citations JSONB）。
type Citation struct {
	TeamID     int64   `json:"team_id"`
	MaterialID int64   `json:"material_id"`
	Chapter    string  `json:"chapter"`
	ChunkIdx   int     `json:"chunk_idx"`
	ChunkID    *int64  `json:"chunk_id,omitempty"`
	Title      string  `json:"title,omitempty"`
	Snippet    string  `json:"snippet,omitempty"`
	Kind       string  `json:"kind,omitempty"`
	PageNumber *int    `json:"page_number,omitempty"`
	Score      float64 `json:"score,omitempty"`
	AssetID    *int64  `json:"asset_id,omitempty"`
}

// BuildHistory 由历史消息构造发给 Agent 的精简历史（最多最近 10 轮）。
func (s *ConversationService) BuildHistory(msgs []model.AgentMessage) []ChatHistory {
	const max = 20
	start := 0
	if len(msgs) > max {
		start = len(msgs) - max
	}
	out := make([]ChatHistory, 0, len(msgs)-start)
	for _, m := range msgs[start:] {
		out = append(out, ChatHistory{Role: m.Role, Content: m.Content})
	}
	return out
}
