package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type registeredHTTPUser struct {
	ID    int64
	Token string
}

func registerHTTPUser(t *testing.T, router *gin.Engine, role string) registeredHTTPUser {
	t.Helper()
	suffix := uuid.NewString()[:8]
	body := `{"email":"http_` + suffix + `@test.dev","password":"password123","display_name":"HTTP 测试","role":"` + role + `"}`
	resp := performRequest(t, router, http.MethodPost, "/api/auth/register", body, "")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var payload struct {
		User struct {
			ID int64 `json:"id"`
		} `json:"user"`
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &payload))
	require.Positive(t, payload.User.ID)
	require.NotEmpty(t, payload.AccessToken)
	return registeredHTTPUser{ID: payload.User.ID, Token: payload.AccessToken}
}
