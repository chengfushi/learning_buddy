package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatForwardsAgentTraceIDToClient(t *testing.T) {
	const traceID = "agent-trace-through-backend-42"
	testAgentHeaderValue := "trace-test-" + t.Name()

	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, testAgentHeaderValue, r.Header.Get("X-Agent-Token"))
		switch r.URL.Path {
		case "/embed":
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"embedding": make([]float64, 1024),
			}))
		case "/chat":
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("X-Trace-ID", traceID)
			_, err := w.Write([]byte("data: {\"type\":\"done\",\"citations\":[]}\n\n"))
			require.NoError(t, err)
		default:
			http.NotFound(w, r)
		}
	}))
	defer agent.Close()

	router, svcs := newTestRouterWithServices(t)
	svcs.Cfg.AgentBaseURL = agent.URL
	svcs.Cfg.AgentSharedSecret = testAgentHeaderValue
	user := registerHTTPUser(t, router, "student")

	resp := performRequest(
		t,
		router,
		http.MethodPost,
		"/api/agent/chat",
		`{"question":"trace id 是否会完整转发？"}`,
		user.Token,
	)

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	assert.Equal(t, traceID, resp.Header().Get("X-Trace-ID"))
}
