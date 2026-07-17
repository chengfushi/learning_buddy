package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuizHidesAnswerGradesOptionKeyAndEnforcesOwner(t *testing.T) {
	const sharedSecret = "quiz-test-secret"
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, sharedSecret, r.Header.Get("X-Agent-Token"))
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/embed":
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"embedding": make([]float64, 1024),
			}))
		case "/quiz":
			require.NoError(t, json.NewEncoder(w).Encode([]map[string]any{{
				"question":   "牛顿第一定律描述什么？",
				"options":    []string{"惯性", "加速度", "质量", "速度"},
				"answer_key": "A",
				"difficulty": "easy",
			}}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer agent.Close()

	router, svcs := newTestRouterWithServices(t)
	svcs.Cfg.AgentBaseURL = agent.URL
	svcs.Cfg.AgentSharedSecret = sharedSecret
	owner := registerHTTPUser(t, router, "student")
	outsider := registerHTTPUser(t, router, "student")

	generated := performRequest(
		t, router, http.MethodPost, "/api/agent/quiz",
		`{"topic":"牛顿定律","count":1}`, owner.Token,
	)
	require.Equal(t, http.StatusOK, generated.Code, generated.Body.String())
	assert.NotContains(t, generated.Body.String(), "AnswerKey")
	assert.NotContains(t, generated.Body.String(), "answer_key")
	assert.NotContains(t, generated.Body.String(), "UserID")

	payload := decodeJSONObject(t, generated.Body.Bytes())
	exercises, ok := payload["exercises"].([]any)
	require.True(t, ok)
	require.Len(t, exercises, 1)
	exercise, ok := exercises[0].(map[string]any)
	require.True(t, ok)
	exerciseID, ok := exercise["ID"].(float64)
	require.True(t, ok)
	options, ok := exercise["Options"].([]any)
	require.True(t, ok, "公开契约应返回选项数组而非 JSONB 字节")
	require.Equal(t, "惯性", options[0])

	answerPath := "/api/agent/quiz/" + strconv.FormatInt(int64(exerciseID), 10) + "/answer"
	ownerAnswer := performRequest(
		t, router, http.MethodPost,
		answerPath,
		`{"choice":"A"}`, owner.Token,
	)
	require.Equal(t, http.StatusOK, ownerAnswer.Code, ownerAnswer.Body.String())
	assert.Equal(t, true, decodeJSONObject(t, ownerAnswer.Body.Bytes())["is_correct"])

	outsiderAnswer := performRequest(
		t, router, http.MethodPost, answerPath, `{"choice":"A"}`, outsider.Token,
	)
	assert.Equal(t, http.StatusNotFound, outsiderAnswer.Code)
}
