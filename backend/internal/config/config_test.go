package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRequiresAgentSharedSecret(t *testing.T) {
	cfg := &Config{}

	err := cfg.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "AGENT_SHARED_SECRET")
}

func TestValidateAcceptsAgentSharedSecret(t *testing.T) {
	cfg := &Config{AgentSharedSecret: "configured"}

	require.NoError(t, cfg.Validate())
}
