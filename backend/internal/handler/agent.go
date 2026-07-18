package handler

import (
	"bufio"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"learning_buddy/backend/internal/middleware"
	"learning_buddy/backend/internal/model"
	"learning_buddy/backend/internal/observability"
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
	var err error
	// 已有会话必须与本次资料作用域一致；新会话在资料鉴权成功后再创建。
	sessionID := req.SessionID
	history := make([]service.ChatHistory, 0)
	if sessionID != "" {
		msgs, historyErr := h.Svc.Conversation.MessagesForScope(ctx, sessionID, uid, req.MaterialID)
		if historyErr != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "会话不存在"})
			return
		}
		history = h.Svc.Conversation.BuildHistory(msgs)
	}
	prepared, err := h.Svc.Agent.PrepareVisibleContext(
		ctx, uid, req.Question, history, req.MaterialID,
	)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "资料不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if sessionID == "" {
		title := truncateRunes(req.Question, 40)
		if len([]rune(req.Question)) > 40 {
			title += "…"
		}
		sessionID, err = h.Svc.Conversation.NewSession(ctx, uid, title, req.MaterialID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	observability.ObserveRAG(prepared.StageMS, prepared.DegradedStages, len(prepared.Chunks) == 0)

	// 落 user 消息
	if _, err := h.Svc.Conversation.AppendMessage(ctx, sessionID, "user", req.Question, nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 调 Agent 流式接口
	stream, err := h.Svc.Agent.ChatStream(ctx, service.ChatRequest{
		Question:       req.Question,
		SessionID:      sessionID,
		History:        history,
		Chunks:         prepared.Chunks,
		Service:        "chat",
		RetrievalQuery: prepared.RetrievalQuery,
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
	meta, _ := json.Marshal(gin.H{
		"type": "meta", "session_id": sessionID, "trace_id": stream.TraceID,
		"rewritten_query": prepared.RetrievalQuery,
		"rewrite_applied": prepared.RewriteApplied,
	})
	_, _ = c.Writer.WriteString("data: " + string(meta) + "\n\n")
	if flusher != nil {
		flusher.Flush()
	}

	var answer strings.Builder
	var citations []service.Citation
	var promptTokens, completionTokens int
	agentDone, agentFailed := false, false
	scanner := bufio.NewScanner(stream.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
streamLoop:
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
			agentDone = true
			citations = validateCitations(ev.Citations, prepared.Chunks)
			promptTokens = ev.PromptTokens
			completionTokens = ev.CompletionTokens
			// done 由 Backend 在回答、追踪落库后补充 message_id 再转发。
			break streamLoop
		case "error":
			agentFailed = true
			_, _ = c.Writer.WriteString("data: " + payload + "\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			break streamLoop
		}
	}
	scanErr := scanner.Err()
	if scanErr != nil {
		slog.Warn("agent chat stream read failed", "session_id", sessionID, "err", scanErr)
	}
	completionErr := agentStreamCompletionError(agentDone, answer.String(), scanErr)
	if !agentFailed && completionErr != "" {
		payload, _ := json.Marshal(gin.H{"type": "error", "text": completionErr})
		_, _ = c.Writer.WriteString("data: " + string(payload) + "\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}

	// 收尾：落 assistant 消息 + token 用量
	var assistantMessage *model.AgentMessage
	if !agentFailed && completionErr == "" {
		assistantMessage, err = h.Svc.Conversation.AppendMessage(
			ctx, sessionID, "assistant", answer.String(), citations,
		)
		if err != nil {
			_, _ = c.Writer.WriteString("data: {\"type\":\"error\",\"text\":\"回答保存失败\"}\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
	traceID := stream.TraceID
	if traceID == "" {
		traceID = uuid.NewString()
	}
	runID := uuid.NewString()
	stageJSON, _ := json.Marshal(prepared.StageMS)
	run := &model.RAGRun{
		ID: runID, UserID: uid, SessionID: &sessionID, TraceID: traceID,
		OriginalQuery: req.Question, RewrittenQuery: prepared.RetrievalQuery,
		RewriteApplied: prepared.RewriteApplied, IndexVersion: prepared.IndexVersion,
		StageDurations: stageJSON, DegradedStages: model.StringArray(prepared.DegradedStages),
	}
	if assistantMessage != nil {
		run.MessageID = &assistantMessage.ID
	}
	selected := make(map[int64]bool, len(prepared.Chunks))
	for _, chunk := range prepared.Chunks {
		selected[chunk.ChunkID] = true
	}
	hits := make([]model.RAGRunHit, 0, len(prepared.Candidates))
	for rank, candidate := range prepared.Candidates {
		chunkID := candidate.ID
		hits = append(hits, model.RAGRunHit{
			RunID: runID, ChunkID: &chunkID, MaterialID: candidate.MaterialID, Rank: rank + 1,
			VectorScore: candidate.VectorScore, LexicalScore: candidate.LexicalScore,
			RRFScore: candidate.RRFScore, RerankScore: candidate.RerankScore,
			Selected: selected[candidate.ID],
		})
	}
	if err := h.Svc.Repos.RecordRAGRun(ctx, run, hits); err != nil {
		// 追踪失败不打断用户回答。
		_ = err
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
	if assistantMessage != nil {
		donePayload, _ := json.Marshal(gin.H{
			"type": "done", "citations": citations, "session_id": sessionID,
			"message_id": assistantMessage.ID,
			"stage_ms":   prepared.StageMS, "degraded_stages": prepared.DegradedStages,
		})
		_, _ = c.Writer.WriteString("data: " + string(donePayload) + "\n\n")
	}
	// 结束事件
	_, _ = c.Writer.WriteString("data: {\"type\":\"end\"}\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func agentStreamCompletionError(done bool, answer string, scanErr error) string {
	if scanErr != nil || !done {
		return "回答生成中断，请重试"
	}
	if strings.TrimSpace(answer) == "" {
		return "当前知识库未返回有效回答，请重试"
	}
	return ""
}

// validateCitations never trusts IDs returned by the model service. Every citation is rebuilt
// from the exact permission-checked context sent to Agent; unknown or altered IDs are discarded.
func validateCitations(raw []service.Citation, chunks []service.Chunk) []service.Citation {
	allowed := make(map[int64]service.Chunk, len(chunks))
	for _, chunk := range chunks {
		allowed[chunk.ChunkID] = chunk
	}
	validated := make([]service.Citation, 0, len(raw))
	seen := make(map[int64]bool, len(raw))
	for _, citation := range raw {
		if citation.ChunkID == nil || seen[*citation.ChunkID] {
			continue
		}
		chunk, ok := allowed[*citation.ChunkID]
		if !ok || chunk.MaterialID != citation.MaterialID {
			continue
		}
		chunkID := chunk.ChunkID
		validated = append(validated, service.Citation{
			TeamID: chunk.TeamID, MaterialID: chunk.MaterialID, Chapter: chunk.Chapter,
			ChunkIdx: chunk.ChunkIdx, ChunkID: &chunkID, Title: chunk.Title,
			Snippet: truncateRunes(chunk.Content, 120), Kind: chunk.Kind,
			PageNumber: chunk.PageNumber, Score: chunk.Score, AssetID: chunk.AssetID,
		})
		seen[chunkID] = true
	}
	return validated
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func (h *Handlers) feedback(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	messageID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || messageID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效回答"})
		return
	}
	var req struct {
		Rating string  `json:"rating"`
		Reason *string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || (req.Rating != "up" && req.Rating != "down") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "rating 必须为 up 或 down"})
		return
	}
	if req.Reason != nil {
		trimmed := strings.TrimSpace(*req.Reason)
		if len([]rune(trimmed)) > 500 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "反馈原因不能超过 500 字"})
			return
		}
		req.Reason = &trimmed
	}
	item, err := h.Svc.Repos.UpsertMessageFeedback(
		c.Request.Context(), uid, messageID, req.Rating, req.Reason,
	)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "回答不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存反馈失败"})
		return
	}
	observability.RecordFeedback(req.Rating)
	c.JSON(http.StatusOK, gin.H{"feedback": item})
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
	c.JSON(http.StatusOK, gin.H{"session_id": sid, "messages": sessionMessageViews(msgs)})
}

type sessionMessageView struct {
	ID        int64
	Role      string
	Content   string
	Citations []service.Citation
	CreatedAt time.Time
}

func sessionMessageViews(messages []model.AgentMessage) []sessionMessageView {
	views := make([]sessionMessageView, 0, len(messages))
	for _, message := range messages {
		citations := make([]service.Citation, 0)
		if len(message.Citations) > 0 {
			if err := json.Unmarshal(message.Citations, &citations); err != nil {
				slog.Warn(
					"conversation citations decode failed",
					"session_id", message.SessionID,
					"message_id", message.ID,
					"err", err,
				)
				citations = make([]service.Citation, 0)
			}
		}
		if citations == nil {
			citations = make([]service.Citation, 0)
		}
		views = append(views, sessionMessageView{
			ID: message.ID, Role: message.Role, Content: message.Content,
			Citations: citations, CreatedAt: message.CreatedAt,
		})
	}
	return views
}
