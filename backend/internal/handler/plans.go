package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"learning_buddy/backend/internal/middleware"
	"learning_buddy/backend/internal/model"
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
	visible, err := h.Svc.Teams.VisibleTeamIDs(c.Request.Context(), uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	res, err := h.Svc.Agent.Plan(c.Request.Context(), req.Goal, req.Deadline, visible)
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
	visible, err := h.Svc.Teams.VisibleTeamIDs(c.Request.Context(), uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items, err := h.Svc.Agent.Quiz(c.Request.Context(), req.Topic, req.MaterialID, req.Count, visible)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Agent 出题失败：" + err.Error()})
		return
	}
	exercises := make([]model.Exercise, 0, len(items))
	for _, it := range items {
		optsJSON, _ := json.Marshal(it.Options)
		ak := it.AnswerKey
		e := model.Exercise{
			MaterialID: req.MaterialID,
			Question:   it.Question,
			Options:    optsJSON,
			AnswerKey:  &ak,
			Difficulty: &it.Difficulty,
		}
		if err := h.Svc.Repos.CreateExercise(c.Request.Context(), &e); err == nil {
			exercises = append(exercises, e)
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
	var e model.Exercise
	if err := h.Svc.Repos.DB.WithContext(c.Request.Context()).First(&e, "id = ?", exerciseID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "题目不存在"})
		return
	}
	isCorrect := e.AnswerKey != nil && strings.EqualFold(strings.TrimSpace(req.Choice), strings.TrimSpace(*e.AnswerKey))
	score := 0.0
	if isCorrect {
		score = 100.0
	}
	ch := req.Choice
	attempt := &model.QuizAttempt{
		UserID:     uid,
		ExerciseID: exerciseID,
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
