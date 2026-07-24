package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"learning_buddy/backend/internal/model"
)

func TestMaterialReadAndNotesRequireRepositoryVisibility(t *testing.T) {
	router, svcs := newTestRouterWithServices(t)
	teacher := registerHTTPUser(t, router, "teacher")
	member := registerHTTPUser(t, router, "student")
	outsider := registerHTTPUser(t, router, "student")

	joinCode := "R2TEST"
	team := model.Team{Name: "权限测试班", Type: "teacher", JoinCode: &joinCode, OwnerID: &teacher.ID}
	require.NoError(t, svcs.Repos.DB.Create(&team).Error)
	draft := model.Material{TeamID: team.ID, Title: "备课草稿", Shared: false, OwnerID: teacher.ID}
	require.NoError(t, svcs.Repos.DB.Create(&draft).Error)
	shared := model.Material{TeamID: team.ID, Title: "学生讲义", Shared: true, OwnerID: teacher.ID}
	require.NoError(t, svcs.Repos.DB.Create(&shared).Error)
	teamID, draftID, sharedID := team.ID, draft.ID, shared.ID

	joinResp := performRequest(
		t,
		router,
		http.MethodPost,
		"/api/teams/join",
		fmt.Sprintf(`{"code":%q}`, joinCode),
		member.Token,
	)
	require.Equal(t, http.StatusOK, joinResp.Code, joinResp.Body.String())
	approveResp := performRequest(
		t,
		router,
		http.MethodPost,
		fmt.Sprintf("/api/teams/%d/members/%d/approve", teamID, member.ID),
		"",
		teacher.Token,
	)
	require.Equal(t, http.StatusOK, approveResp.Code, approveResp.Body.String())

	t.Run("nonmember cannot read a shared teacher material by guessed id", func(t *testing.T) {
		resp := performRequest(t, router, http.MethodGet, fmt.Sprintf("/api/materials/%d", sharedID), "", outsider.Token)
		assert.Equal(t, http.StatusNotFound, resp.Code, resp.Body.String())
	})

	t.Run("approved member reads shared material but not teacher draft", func(t *testing.T) {
		sharedResp := performRequest(t, router, http.MethodGet, fmt.Sprintf("/api/materials/%d", sharedID), "", member.Token)
		assert.Equal(t, http.StatusOK, sharedResp.Code, sharedResp.Body.String())

		draftResp := performRequest(t, router, http.MethodGet, fmt.Sprintf("/api/materials/%d", draftID), "", member.Token)
		assert.Equal(t, http.StatusNotFound, draftResp.Code, draftResp.Body.String())
	})

	t.Run("teacher owner reads own draft", func(t *testing.T) {
		resp := performRequest(t, router, http.MethodGet, fmt.Sprintf("/api/materials/%d", draftID), "", teacher.Token)
		assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	})

	t.Run("non-owner cannot update or delete teacher material", func(t *testing.T) {
		updateResp := performRequest(
			t, router, http.MethodPut, fmt.Sprintf("/api/materials/%d", sharedID),
			`{"title":"越权修改"}`, member.Token,
		)
		assert.Equal(t, http.StatusForbidden, updateResp.Code, updateResp.Body.String())

		deleteResp := performRequest(
			t, router, http.MethodDelete, fmt.Sprintf("/api/materials/%d", sharedID), "", member.Token,
		)
		assert.Equal(t, http.StatusForbidden, deleteResp.Code, deleteResp.Body.String())

		outsiderDelete := performRequest(
			t, router, http.MethodDelete, fmt.Sprintf("/api/materials/%d", sharedID), "", outsider.Token,
		)
		assert.Equal(t, http.StatusForbidden, outsiderDelete.Code, outsiderDelete.Body.String())
	})

	t.Run("nonmember team view returns no materials", func(t *testing.T) {
		resp := performRequest(t, router, http.MethodGet, fmt.Sprintf("/api/teams/%d/materials", teamID), "", outsider.Token)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		var payload struct {
			Materials []model.Material `json:"materials"`
		}
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &payload))
		assert.Empty(t, payload.Materials)
	})

	t.Run("nonmember cannot create or list notes on shared material", func(t *testing.T) {
		createResp := performRequest(
			t,
			router,
			http.MethodPost,
			fmt.Sprintf("/api/materials/%d/notes", sharedID),
			`{"content":"越权笔记"}`,
			outsider.Token,
		)
		assert.Equal(t, http.StatusNotFound, createResp.Code, createResp.Body.String())

		listResp := performRequest(t, router, http.MethodGet, fmt.Sprintf("/api/materials/%d/notes", sharedID), "", outsider.Token)
		assert.Equal(t, http.StatusNotFound, listResp.Code, listResp.Body.String())
	})

	t.Run("nonmember cannot generate quiz from shared material by guessed id", func(t *testing.T) {
		resp := performRequest(
			t,
			router,
			http.MethodPost,
			"/api/agent/quiz",
			fmt.Sprintf(`{"topic":"越权测试","material_id":%d,"count":1}`, sharedID),
			outsider.Token,
		)
		assert.Equal(t, http.StatusNotFound, resp.Code, resp.Body.String())
	})

	t.Run("member notes follow material shared visibility", func(t *testing.T) {
		allowedResp := performRequest(
			t,
			router,
			http.MethodPost,
			fmt.Sprintf("/api/materials/%d/notes", sharedID),
			`{"content":"课堂笔记"}`,
			member.Token,
		)
		assert.Equal(t, http.StatusOK, allowedResp.Code, allowedResp.Body.String())

		deniedResp := performRequest(
			t,
			router,
			http.MethodPost,
			fmt.Sprintf("/api/materials/%d/notes", draftID),
			`{"content":"不应写入"}`,
			member.Token,
		)
		assert.Equal(t, http.StatusNotFound, deniedResp.Code, deniedResp.Body.String())
	})

	t.Run("teacher owner can note own draft", func(t *testing.T) {
		resp := performRequest(
			t,
			router,
			http.MethodPost,
			fmt.Sprintf("/api/materials/%d/notes", draftID),
			`{"content":"备课备注"}`,
			teacher.Token,
		)
		assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	})
}
