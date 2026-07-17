package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"learning_buddy/backend/internal/model"
	"learning_buddy/backend/internal/service"
)

func TestLearningHTTPContractAndUserIsolation(t *testing.T) {
	router := newTestRouter(t)
	student := registerHTTPUser(t, router, "student")
	other := registerHTTPUser(t, router, "student")

	t.Run("all learning endpoints require authentication", func(t *testing.T) {
		requests := []struct {
			method string
			path   string
			body   string
		}{
			{method: http.MethodPost, path: "/api/learning/records", body: `{}`},
			{method: http.MethodGet, path: "/api/learning/records"},
			{method: http.MethodGet, path: "/api/learning/progress"},
		}
		for _, request := range requests {
			resp := performRequest(t, router, request.method, request.path, request.body, "")
			assert.Equal(t, http.StatusUnauthorized, resp.Code)
		}
	})

	t.Run("malformed record request is rejected", func(t *testing.T) {
		resp := performRequest(
			t,
			router,
			http.MethodPost,
			"/api/learning/records",
			`{"duration_s":"not-an-int"}`,
			student.Token,
		)
		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assertJSONField(t, resp.Body.Bytes(), "error", "请求格式错误")
	})

	first := createLearningRecordHTTP(t, router, student.Token, `{"duration_s":120,"progress":50,"score":80}`)
	second := createLearningRecordHTTP(t, router, student.Token, `{"duration_s":180,"progress":70}`)
	otherRecord := createLearningRecordHTTP(t, router, other.Token, `{"duration_s":600,"progress":100,"score":99}`)

	assert.Equal(t, student.ID, first.UserID)
	assert.Equal(t, 120, first.DurationS)
	assert.Equal(t, 50.0, first.Progress)
	require.NotNil(t, first.Score)
	assert.Equal(t, 80.0, *first.Score)
	assert.Nil(t, second.Score)
	assert.Equal(t, other.ID, otherRecord.UserID)

	t.Run("record list contains only the authenticated user rows", func(t *testing.T) {
		resp := performRequest(t, router, http.MethodGet, "/api/learning/records", "", student.Token)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		var payload struct {
			Records []model.LearningRecord `json:"records"`
		}
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &payload))
		require.Len(t, payload.Records, 2)
		for _, record := range payload.Records {
			assert.Equal(t, student.ID, record.UserID)
			assert.NotEqual(t, otherRecord.ID, record.ID)
		}
	})

	t.Run("progress aggregates seconds and percent values without another user data", func(t *testing.T) {
		resp := performRequest(t, router, http.MethodGet, "/api/learning/progress", "", student.Token)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		var payload struct {
			Summary service.ProgressSummary `json:"summary"`
		}
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &payload))
		assert.Equal(t, 300, payload.Summary.TotalDurationS)
		assert.Equal(t, 60.0, payload.Summary.AvgProgress)
		assert.Zero(t, payload.Summary.QuizCount)
		assert.Zero(t, payload.Summary.QuizCorrect)
		assert.Zero(t, payload.Summary.QuizAccuracy)
		require.Len(t, payload.Summary.Daily, 1)
		assert.Equal(t, 300, payload.Summary.Daily[0].Duration)
		assert.Equal(t, 60.0, payload.Summary.Daily[0].Progress)
	})

	t.Run("new user progress is an explicit zero summary", func(t *testing.T) {
		fresh := registerHTTPUser(t, router, "student")
		resp := performRequest(t, router, http.MethodGet, "/api/learning/progress", "", fresh.Token)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		var payload struct {
			Summary service.ProgressSummary `json:"summary"`
		}
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &payload))
		assert.Zero(t, payload.Summary.TotalDurationS)
		assert.Zero(t, payload.Summary.AvgProgress)
		assert.Empty(t, payload.Summary.Daily)
	})
}

func createLearningRecordHTTP(t *testing.T, router http.Handler, token, body string) model.LearningRecord {
	t.Helper()
	resp := performRequest(t, router, http.MethodPost, "/api/learning/records", body, token)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var payload struct {
		Record model.LearningRecord `json:"record"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &payload))
	require.Positive(t, payload.Record.ID)
	return payload.Record
}
