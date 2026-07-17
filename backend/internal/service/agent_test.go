package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"learning_buddy/backend/internal/config"
	"learning_buddy/backend/internal/repository"
)

func TestAgentServiceAddsSharedSecretToEveryRequest(t *testing.T) {
	const secret = "unit-test-agent-secret"
	var mu sync.Mutex
	requestedPaths := make([]string, 0, 5)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, secret, r.Header.Get(agentTokenHeader))
		mu.Lock()
		requestedPaths = append(requestedPaths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/parse":
			_, _ = w.Write([]byte(`{}`))
		case "/embed":
			_, _ = w.Write([]byte(`{"embedding":[0.1,0.2]}`))
		case "/chat":
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("X-Trace-ID", "agent-trace-42")
			_, _ = w.Write([]byte("data: {\"type\":\"done\"}\n\n"))
		case "/plan":
			_, _ = w.Write([]byte(`{"title":"test","goal":"test","items":[]}`))
		case "/quiz":
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := &config.Config{AgentBaseURL: server.URL, AgentSharedSecret: secret}
	agent := NewAgentService(cfg, repository.New(nil))
	ctx := context.Background()

	require.NoError(t, agent.Parse(ctx, 1, 7, "content", "txt", ""))
	_, err := postJSON[struct {
		Embedding []float64 `json:"embedding"`
	}](ctx, agent.client, cfg, "/embed", EmbedRequest{Text: "query"})
	require.NoError(t, err)
	stream, err := agent.ChatStream(ctx, ChatRequest{Question: "question"})
	require.NoError(t, err)
	assert.Equal(t, "agent-trace-42", stream.TraceID)
	_, err = io.Copy(io.Discard, stream.Body)
	require.NoError(t, err)
	require.NoError(t, stream.Body.Close())
	_, err = postJSON[PlanResult](ctx, agent.client, cfg, "/plan", PlanRequest{Goal: "goal"})
	require.NoError(t, err)
	_, err = postJSON[[]QuizItem](ctx, agent.client, cfg, "/quiz", QuizRequest{Topic: "topic", Count: 1})
	require.NoError(t, err)

	mu.Lock()
	gotPaths := append([]string(nil), requestedPaths...)
	mu.Unlock()
	assert.Equal(t, []string{"/parse", "/embed", "/chat", "/plan", "/quiz"}, gotPaths)
}

func TestAgentServiceRejectsMissingSharedSecretBeforeNetworkCall(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	agent := NewAgentService(&config.Config{AgentBaseURL: server.URL}, repository.New(nil))
	err := agent.Parse(context.Background(), 1, 1, "content", "txt", "")

	assert.ErrorIs(t, err, errAgentSharedSecretMissing)
	assert.Zero(t, requestCount)
}

func TestRetrieveVisibleContextEmbeddingTimeoutDegradesToEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{"embedding":[0.1]}`))
	}))
	defer server.Close()

	cfg := &config.Config{AgentBaseURL: server.URL, AgentSharedSecret: "timeout-secret"}
	agent := NewAgentService(cfg, repository.New(nil))
	agent.retrieveTimeout = 10 * time.Millisecond

	started := time.Now()
	chunks, err := agent.RetrieveVisibleContext(context.Background(), 1, "query", nil, 5)

	require.NoError(t, err)
	assert.Empty(t, chunks)
	assert.Less(t, time.Since(started), 200*time.Millisecond)
}
