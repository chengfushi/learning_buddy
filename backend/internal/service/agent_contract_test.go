package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type contractExchange struct {
	Request  json.RawMessage `json:"request"`
	Response json.RawMessage `json:"response"`
}

type agentContract struct {
	Parse contractExchange `json:"parse"`
	Embed contractExchange `json:"embed"`
	Chat  contractExchange `json:"chat"`
	Plan  contractExchange `json:"plan"`
	Quiz  contractExchange `json:"quiz"`
}

func loadAgentContract(t *testing.T) agentContract {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	path := filepath.Join(filepath.Dir(filename), "..", "..", "..", "tests", "contracts", "agent_api.json")
	// #nosec G304 -- path is a repository fixture derived from this source file's location.
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var contract agentContract
	require.NoError(t, json.Unmarshal(data, &contract))
	return contract
}

func assertJSONRoundTrip[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var value T
	require.NoError(t, json.Unmarshal(raw, &value))
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	assert.JSONEq(t, string(raw), string(encoded))
	return value
}

func TestAgentContractFixtureMatchesBackendDTOs(t *testing.T) {
	contract := loadAgentContract(t)

	assertJSONRoundTrip[ParseRequest](t, contract.Parse.Request)
	assertJSONRoundTrip[EmbedRequest](t, contract.Embed.Request)
	assertJSONRoundTrip[ChatRequest](t, contract.Chat.Request)
	assertJSONRoundTrip[PlanRequest](t, contract.Plan.Request)
	assertJSONRoundTrip[QuizRequest](t, contract.Quiz.Request)

	var parseResponse struct {
		MaterialID int64  `json:"material_id"`
		Chunks     int    `json:"chunks"`
		Status     string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(contract.Parse.Response, &parseResponse))
	assert.Equal(t, int64(42), parseResponse.MaterialID)

	var embedResponse struct {
		Embedding []float64 `json:"embedding"`
	}
	require.NoError(t, json.Unmarshal(contract.Embed.Response, &embedResponse))
	require.NotEmpty(t, embedResponse.Embedding)

	var chatResponse struct {
		Answer    string     `json:"answer"`
		Citations []Citation `json:"citations"`
	}
	require.NoError(t, json.Unmarshal(contract.Chat.Response, &chatResponse))
	require.NotEmpty(t, chatResponse.Answer)
	require.Len(t, chatResponse.Citations, 1)

	assertJSONRoundTrip[PlanResult](t, contract.Plan.Response)
	var quizResponse []QuizItem
	require.NoError(t, json.Unmarshal(contract.Quiz.Response, &quizResponse))
	require.Len(t, quizResponse, 1)
}
