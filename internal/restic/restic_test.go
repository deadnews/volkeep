package restic

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBaseEnv(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{
		"RESTIC_REPOSITORY=s3:h/b",
		"RESTIC_PASSWORD=pw",
	}, BaseEnv("s3:h/b", "pw"))
}

func TestAwsEnv(t *testing.T) {
	t.Parallel()
	environ := []string{"PATH=/bin", "AWS_ACCESS_KEY_ID=id", "HOME=/root", "AWS_SESSION_TOKEN=tok"}
	assert.Equal(t, []string{"AWS_ACCESS_KEY_ID=id", "AWS_SESSION_TOKEN=tok"}, AwsEnv(environ))
	assert.Nil(t, AwsEnv([]string{"PATH=/bin"}))
}

func TestRcloneEnv(t *testing.T) {
	t.Parallel()
	environ := []string{"PATH=/bin", "RCLONE_CONFIG_R_TYPE=s3", "HOME=/root", "RCLONE_CONFIG_R_ACCESS_KEY_ID=id"}
	assert.Equal(t, []string{"RCLONE_CONFIG_R_TYPE=s3", "RCLONE_CONFIG_R_ACCESS_KEY_ID=id"}, RcloneEnv(environ))
	assert.Nil(t, RcloneEnv([]string{"PATH=/bin"}))
}

func TestArgs(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"init"}, InitArgs())
	assert.Equal(t, []string{"cat", "config", "--no-lock"}, CatConfigArgs())
	assert.Equal(t, []string{"unlock"}, UnlockArgs())
	assert.Equal(t,
		[]string{"backup", "/data", "--host", "h1", "--tag", "rss2tg", "--json", quiet},
		BackupArgs("h1", "rss2tg"),
	)
	assert.Equal(t,
		[]string{"forget", quiet, "--tag", "rss2tg", "--keep-daily", "3"},
		ForgetArgs("rss2tg", 3),
	)
	assert.Equal(t, []string{"forget", quiet, "--keep-within", "30d"}, SweepArgs(30))
	assert.Equal(t, []string{"prune", quiet}, PruneArgs())
	assert.Equal(t, []string{"check", quiet}, CheckArgs())
	assert.Equal(t, []string{"stats", "--mode", "raw-data", "--json"}, StatsArgs())
}
