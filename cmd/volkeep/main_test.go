package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRun_FailsOnMissingConfig: a missing required env var must error before any Docker call.
func TestRun_FailsOnMissingConfig(t *testing.T) {
	for _, k := range []string{
		"VOLKEEP_SCHEDULE", "RESTIC_PASSWORD", "RESTIC_REPOSITORY",
		"VOLKEEP_RETENTION_DAYS", "VOLKEEP_JITTER", "VOLKEEP_HOST",
		"VOLKEEP_RESTIC_IMAGE",
	} {
		t.Setenv(k, "")
	}

	err := run()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "VOLKEEP_SCHEDULE")
}
