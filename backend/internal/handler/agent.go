package handler

import (
	"bufio"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"learning_buddy/backend/internal/middleware"
	"learning_buddy/backend/internal/model"
	"learning_buddy/backend/internal/repository"
	"learning_buddy/backend/internal/service"
)

// chatReq 答疑请求（F4）。
type chatReq struct {
	Question   string `json:"question"`
	SessionID  string `json:"session_id"`
	MaterialID *int64 `json:"material_id"`
}

// chat 流式答疑：SSE 转发 Agent 输出，并在收尾时落库（会话/消息/引用/token 用量）。
// 安全：Backend repository 先检索出已授权 chunks，Agent 不接触可见性谓词。
func (h *Handlers) chat(c *gin.Context) {
	uid := middleware.CtxUserID(c)

	var req chatReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Question == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 question"})
		return
	}

	ctx := c.Request.Context()
	chunks, err := h.Svc.Agent.RetrieveVisibleContext(ctx, uid, req.Question, req.MaterialID, 5)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "资料不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 会话：复用或新建
	sessionID := req.SessionID
	if sessionID == "" {
		title := req.Question
		if len(title) > 40 {
			title = title[:40] + "…"
		}
		sessionID, err = h.Svc.Conversation.NewSession(ctx, uid, title)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	// 历史（用于多轮上下文）
	msgs, _ := h.Svc.Conversation.Messages(ctx, sessionID, uid)
	history := h.Svc.Conversation.BuildHistory(msgs)

	// 落 user 消息
	if _, err := h.Svc.Conversation.AppendMessage(ctx, sessionID, "user", req.Question, nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 调 Agent 流式接口
	stream, err := h.Svc.Agent.ChatStream(ctx, service.ChatRequest{
		Question:  req.Question,
		SessionID: sessionID,
		History:   history,
		Chunks:    chunks,
		Service:   "chat",
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Agent 服务不可用：" + err.Error()})
		return
	}
	defer func() { _ = stream.Body.Close() }()

	// SSE 响应头
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	if stream.TraceID != "" {
		c.Writer.Header().Set("X-Trace-ID", stream.TraceID)
	}
	c.Writer.WriteHeader(http.StatusOK)

	flusher, _ := c.Writer.(http.Flusher)

	var answer strings.Builder
	var citations []service.Citation
	var promptTokens, completionTokens int
	scanner := bufio.NewScanner(stream.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		var ev struct {
			Type             string             `json:"type"`
			Text             string             `json:"text"`
			Citations        []service.Citation `json:"citations"`
			PromptTokens     int                `json:"prompt_tokens"`
			CompletionTokens int                `json:"completion_tokens"`
			Message          string             `json:"message"`
		}
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "token":
			answer.WriteString(ev.Text)
			// 转发给前端
			_, _ = c.Writer.WriteString("data: " + payload + "\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		case "done":
			citations = ev.Citations
			promptTokens = ev.PromptTokens
			completionTokens = ev.CompletionTokens
			_, _ = c.Writer.WriteString("data: " + payload + "\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		case "error":
			_, _ = c.Writer.WriteString("data: " + payload + "\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
	}

	// 收尾：落 assistant 消息 + token 用量
	if answer.Len() > 0 {
		citeJSON, _ := json.Marshal(citations)
		_ = h.Svc.Repos.DB.WithContext(ctx).Create(&model.AgentMessage{
			SessionID: sessionID,
			Role:      "assistant",
			Content:   answer.String(),
			Citations: citeJSON,
		}).Error
	}
	if promptTokens > 0 || completionTokens > 0 {
		_ = h.Svc.Repos.RecordTokenUsage(ctx, &model.TokenUsage{
			UserID:           uid,
			Service:          "chat",
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		})
	}
	// 结束事件
	_, _ = c.Writer.WriteString("data: {\"type\":\"end\"}\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func (h *Handlers) listSessions(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	sessions, err := h.Svc.Conversation.ListSessions(c.Request.Context(), uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"sessions": sessions})
}

func (h *Handlers) getSession(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	sid := c.Param("id")
	msgs, err := h.Svc.Conversation.Messages(c.Request.Context(), sid, uid)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "会话不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"session_id": sid, "messages": msgs})
}
