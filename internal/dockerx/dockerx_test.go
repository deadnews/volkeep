package dockerx

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunDemuxesLogs guards against raw multiplexed frame headers leaking
// into Logs; demuxed text carries no NUL bytes.
func TestRunDemuxesLogs(t *testing.T) {
	if os.Getenv("TESTCONTAINERS") != "1" {
		t.Skip("Skipping integration test, set TESTCONTAINERS=1 to run it.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	dx, err := New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = dx.Close() })

	const img = "busybox:musl"
	require.NoError(t, dx.Pull(ctx, img))

	res, err := dx.Run(ctx, &RunSpec{
		Image: img,
		Args:  []string{"sh", "-c", "echo out; echo err >&2"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, res.ExitCode)
	assert.Contains(t, res.Logs, "out")
	assert.Contains(t, res.Logs, "err")
	assert.NotContains(t, res.Logs, "\x00")
}
