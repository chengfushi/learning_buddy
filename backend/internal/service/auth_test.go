package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"learning_buddy/backend/internal/config"
	"learning_buddy/backend/internal/model"
)

func TestAuthRegisterValidation(t *testing.T) {
	auth := NewAuthService(nil, &config.Config{JWTSecret: "test"})

	tests := []struct {
		name        string
		email       string
		password    string
		role        string
		wantMessage string
	}{
		{name: "missing email", password: "password123", wantMessage: "邮箱与密码必填"},
		{name: "missing password", email: "student@example.com", wantMessage: "邮箱与密码必填"},
		{name: "reserved administrator role", email: "admin@example.com", password: "password123", role: "super_admin", wantMessage: "不允许注册该角色"},
		{name: "unknown role", email: "unknown@example.com", password: "password123", role: "unknown", wantMessage: "不允许注册该角色"},
		{name: "short password", email: "student@example.com", password: "12345", role: "student", wantMessage: "密码至少 6 位"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := auth.Register(context.Background(), tt.email, tt.password, "测试用户", tt.role)

			require.EqualError(t, err, tt.wantMessage)
			assert.Nil(t, user)
		})
	}
}

func TestAuthRegisterLoginAndRefreshLifecycle(t *testing.T) {
	svcs, db := newTestServices(t)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	svcs.Repos.DB = tx

	ctx := context.Background()
	suffix := uuid.NewString()[:8]
	email := "auth_" + suffix + "@test.dev"
	password := "password123"

	user, err := svcs.Auth.Register(ctx, email, password, "认证测试", "")
	require.NoError(t, err)
	assert.Equal(t, "student", user.Role, "空角色必须安全降级为 student")
	assert.Equal(t, "free", user.Subscription)
	require.NotNil(t, user.PasswordHash)
	assert.NotEqual(t, password, *user.PasswordHash)
	duplicate, err := svcs.Auth.Register(ctx, email, password, "重复账号", "student")
	require.Error(t, err)
	assert.Nil(t, duplicate)

	var privateTeamCount int64
	require.NoError(t, tx.Model(&model.Team{}).
		Where("owner_id = ? AND type = ?", user.ID, "private").
		Count(&privateTeamCount).Error)
	assert.Equal(t, int64(1), privateTeamCount, "注册事务必须同时创建唯一的私有资料团队")

	loggedIn, access, refresh, err := svcs.Auth.Login(ctx, email, password)
	require.NoError(t, err)
	assert.Equal(t, user.ID, loggedIn.ID)
	assert.NotEmpty(t, access)
	assert.NotEmpty(t, refresh)
	assert.NotEqual(t, access, refresh)
	stored, err := svcs.Repos.FindRefreshToken(ctx, hashRefreshToken(refresh))
	require.NoError(t, err)
	assert.NotEqual(t, refresh, stored.TokenHash, "数据库不得保存 refresh token 明文")

	accessClaims, err := svcs.Auth.VerifyToken(access)
	require.NoError(t, err)
	assert.Equal(t, user.ID, accessClaims.UserID)
	assert.Equal(t, "student", accessClaims.Role)
	assert.Equal(t, strconv.FormatInt(user.ID, 10), accessClaims.Subject)

	refreshedAccess, rotatedRefresh, err := svcs.Auth.Refresh(ctx, refresh)
	require.NoError(t, err)
	assert.NotEmpty(t, rotatedRefresh)
	assert.NotEqual(t, refresh, rotatedRefresh)
	refreshedClaims, err := svcs.Auth.VerifyToken(refreshedAccess)
	require.NoError(t, err)
	assert.Equal(t, user.ID, refreshedClaims.UserID)
	assert.Equal(t, user.Role, refreshedClaims.Role)
	_, _, replayErr := svcs.Auth.Refresh(ctx, refresh)
	assert.EqualError(t, replayErr, "refresh token replay detected")
	_, _, familyErr := svcs.Auth.Refresh(ctx, rotatedRefresh)
	assert.EqualError(t, familyErr, "无效 refresh token")
}

func TestAuthLoginRejectsUnknownInvalidAndPasswordlessAccounts(t *testing.T) {
	svcs, db := newTestServices(t)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	svcs.Repos.DB = tx

	ctx := context.Background()
	suffix := uuid.NewString()[:8]
	email := "login_" + suffix + "@test.dev"
	_, err := svcs.Auth.Register(ctx, email, "password123", "登录测试", "student")
	require.NoError(t, err)

	tests := []struct {
		name     string
		email    string
		password string
	}{
		{name: "unknown account", email: "missing_" + suffix + "@test.dev", password: "password123"},
		{name: "wrong password", email: email, password: "incorrect-password"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, access, refresh, loginErr := svcs.Auth.Login(ctx, tt.email, tt.password)

			require.EqualError(t, loginErr, "账号或密码错误")
			assert.Nil(t, user)
			assert.Empty(t, access)
			assert.Empty(t, refresh)
		})
	}

	passwordless := &model.User{
		Email:        "passwordless_" + suffix + "@test.dev",
		DisplayName:  "无密码账号",
		Role:         "student",
		Subscription: "free",
	}
	require.NoError(t, tx.Create(passwordless).Error)
	user, access, refresh, err := svcs.Auth.Login(ctx, passwordless.Email, "password123")
	require.EqualError(t, err, "账号或密码错误")
	assert.Nil(t, user)
	assert.Empty(t, access)
	assert.Empty(t, refresh)
}

func TestAuthTokenValidationRejectsInvalidTokens(t *testing.T) {
	auth := NewAuthService(nil, &config.Config{JWTSecret: "correct-secret"})
	other := NewAuthService(nil, &config.Config{JWTSecret: "different-secret"})

	valid, err := auth.sign(9, "teacher", time.Hour)
	require.NoError(t, err)
	_, err = other.VerifyToken(valid)
	assert.Error(t, err, "使用不同密钥签发的 token 必须被拒绝")

	expired, err := auth.sign(9, "teacher", -time.Hour)
	require.NoError(t, err)
	_, err = auth.VerifyToken(expired)
	assert.Error(t, err, "过期 token 必须被拒绝")

	claims := Claims{
		UserID: 9,
		Role:   "teacher",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	noneToken, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)
	_, err = auth.VerifyToken(noneToken)
	assert.Error(t, err, "非 HMAC 签名算法必须被拒绝")

	for _, token := range []string{"", "not-a-jwt", "a.b.c"} {
		_, err = auth.VerifyToken(token)
		assert.Error(t, err)
		refreshed, rotated, refreshErr := auth.Refresh(context.Background(), token)
		assert.EqualError(t, refreshErr, "无效 refresh token")
		assert.Empty(t, refreshed)
		assert.Empty(t, rotated)
	}
}
