package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"learning_buddy/backend/internal/config"
)

func TestRateLimiterUsesUserScopedSlidingWindow(t *testing.T) {
	limiter := NewRateLimiter(&config.Config{ChatRateLimitPerMin: 2})
	ctx := context.Background()

	ok, err := limiter.AllowChat(ctx, 1)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = limiter.AllowChat(ctx, 1)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = limiter.AllowChat(ctx, 1)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = limiter.AllowChat(ctx, 2)
	require.NoError(t, err)
	require.True(t, ok, "不同用户必须使用独立限流窗口")
}

func TestRateLimiterSeparatesAIServiceWindows(t *testing.T) {
	limiter := NewRateLimiter(&config.Config{
		ChatRateLimitPerMin: 10,
		PlanRateLimitPerMin: 1,
		QuizRateLimitPerMin: 2,
	})
	ctx := context.Background()

	ok, err := limiter.Allow(ctx, 7, "plan")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = limiter.Allow(ctx, 7, "plan")
	require.NoError(t, err)
	require.False(t, ok)

	ok, err = limiter.Allow(ctx, 7, "quiz")
	require.NoError(t, err)
	require.True(t, ok, "Plan 限流不得阻塞 Quiz")
}
