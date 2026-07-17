package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"learning_buddy/backend/internal/middleware"
	"learning_buddy/backend/internal/model"
	"learning_buddy/backend/internal/repository"
)

// ---- 学习计划（F7）----

type planReq struct {
	Goal     string `json:"goal"`
	Deadline string `json:"deadline"` // 2006-01-02
	Title    string `json:"title"`
}

func (h *Handlers) createPlan(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	var req planReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Goal == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供学习目标 goal"})
		return
	}
	res, err := h.Svc.Agent.Plan(c.Request.Context(), uid, req.Goal, req.Deadline)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Agent 规划失败：" + err.Error()})
		return
	}
	itemsJSON, _ := json.Marshal(res.Items)
	title := req.Title
	if title == "" {
		title = res.Title
	}
	plan := &model.StudyPlan{
		UserID:   uid,
		Title:    title,
		Goal:     &req.Goal,
		Deadline: parseDate(req.Deadline),
		Items:    itemsJSON,
	}
	if err := h.Svc.Repos.CreateStudyPlan(c.Request.Context(), plan); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_ = h.Svc.Repos.RecordTokenUsage(c.Request.Context(), &model.TokenUsage{
		UserID: uid, Service: "plan", TotalTokens: 1,
	})
	c.JSON(http.StatusOK, gin.H{"plan": plan})
}

// ---- 智能测评（F8）----

type quizReq struct {
	Topic      string `json:"topic"`
	MaterialID *int64 `json:"material_id"`
	Count      int    `json:"count"`
}

// exerciseResponse 是对学习者的公开题目契约，故意不包含 AnswerKey 与 UserID。
type exerciseResponse struct {
	ID         int64
	MaterialID *int64
	Question   string
	Options    []string
	Difficulty *string
	CreatedAt  time.Time
}

func (h *Handlers) createQuiz(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	var req quizReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	if req.Count <= 0 {
		req.Count = 5
	}
	items, err := h.Svc.Agent.Quiz(c.Request.Context(), uid, req.Topic, req.MaterialID, req.Count)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "资料不存在"})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "Agent 出题失败：" + err.Error()})
		return
	}
	exercises := make([]exerciseResponse, 0, len(items))
	for _, it := range items {
		optsJSON, _ := json.Marshal(it.Options)
		ak := it.AnswerKey
		e := model.Exercise{
			UserID:     uid,
			MaterialID: req.MaterialID,
			Question:   it.Question,
			Options:    optsJSON,
			AnswerKey:  &ak,
			Difficulty: &it.Difficulty,
		}
		if err := h.Svc.Repos.CreateExercise(c.Request.Context(), &e); err == nil {
			exercises = append(exercises, exerciseResponse{
				ID: e.ID, MaterialID: e.MaterialID, Question: e.Question,
				Options: it.Options, Difficulty: e.Difficulty, CreatedAt: e.CreatedAt,
			})
		}
	}
	_ = h.Svc.Repos.RecordTokenUsage(c.Request.Context(), &model.TokenUsage{
		UserID: uid, Service: "quiz", TotalTokens: 1,
	})
	c.JSON(http.StatusOK, gin.H{"exercises": exercises})
}

type answerReq struct {
	Choice string `json:"choice"`
}

func (h *Handlers) answerQuiz(c *gin.Context) {
	uid := middleware.CtxUserID(c)
	exerciseID, err := bindID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效题目"})
		return
	}
	var req answerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 choice"})
		return
	}
	e, err := h.Svc.Repos.GetExerciseForUser(c.Request.Context(), exerciseID, uid)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "题目不存在"})
		return
	}
	choice := strings.ToUpper(strings.TrimSpace(req.Choice))
	if len(choice) != 1 || choice[0] < 'A' || choice[0] > 'D' {
		c.JSON(http.StatusBadRequest, gin.H{"error": "choice 必须为 A/B/C/D"})
		return
	}
	isCorrect := e.AnswerKey != nil && choice == strings.ToUpper(strings.TrimSpace(*e.AnswerKey))
	score := 0.0
	if isCorrect {
		score = 100.0
	}
	ch := choice
	attempt := &model.QuizAttempt{
		UserID:     uid,
		ExerciseID: e.ID,
		Choice:     &ch,
		IsCorrect:  &isCorrect,
		Score:      &score,
	}
	if err := h.Svc.Repos.CreateQuizAttempt(c.Request.Context(), attempt); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"is_correct":  isCorrect,
		"correct_key": e.AnswerKey,
		"attempt":     attempt,
	})
}

func parseDate(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &t
}

func lowerEqual(a, b string) bool {
	return strings.EqualFold(a, b)
}

var _ = lowerEqual
