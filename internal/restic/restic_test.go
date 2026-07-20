package restic

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWorkerEnv(t *testing.T) {
	t.Parallel()

	environ := []string{
		"PATH=/bin",
		"AWS_ACCESS_KEY_ID=id",
		"HOME=/root",
		"RCLONE_CONFIG_R_TYPE=s3",
		"RESTIC_COMPRESSION=max",
		"RESTIC_REPOSITORY=volume:stale",
		"RESTIC_PASSWORD=stale",
	}
	assert.Equal(t, []string{
		"RESTIC_REPOSITORY=s3:h/b",
		"RESTIC_PASSWORD=pw",
		"AWS_ACCESS_KEY_ID=id",
		"RCLONE_CONFIG_R_TYPE=s3",
		"RESTIC_COMPRESSION=max",
	}, WorkerEnv("s3:h/b", "pw", environ), "resolved credentials supersede the daemon's copies")

	assert.Equal(t, []string{
		"RESTIC_REPOSITORY=/repo",
		"RESTIC_PASSWORD=pw",
	}, WorkerEnv("/repo", "pw", []string{"PATH=/bin"}))
}

func TestArgs(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"init"}, InitArgs())
	assert.Equal(t, []string{"cat", "config", "--no-lock"}, CatConfigArgs())
	assert.Equal(t, []string{"unlock"}, UnlockArgs())
	assert.Equal(
		t,
		[]string{"backup", "/data", "--host", "h1", "--tag", "rss2tg", "--json", "--quiet"},
		BackupArgs("h1", "rss2tg"),
	)
	assert.Equal(
		t,
		[]string{"forget", "--tag", "rss2tg", "--keep-daily", "3", "--quiet"},
		ForgetArgs("rss2tg", 3),
	)
	assert.Equal(t, []string{"forget", "--keep-within", "30d", "--quiet"}, SweepArgs(30))
	assert.Equal(t, []string{"prune", "--quiet"}, PruneArgs())
	assert.Equal(t, []string{"check", "--quiet"}, CheckArgs())
	assert.Equal(t, []string{"stats", "--mode", "raw-data", "--json"}, StatsArgs())
}
