package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestController_IsCephHealthOK(t *testing.T) {
	restConfig, err := LoadKubeConfig("")
	require.NoError(t, err)

	log := zaptest.NewLogger(t)
	ctrl, err := NewController(true, restConfig, log)
	require.NoError(t, err)

	healthOK, err := ctrl.IsCephHealthOK()
	require.NoError(t, err)
	assert.True(t, healthOK)
}
