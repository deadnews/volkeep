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

// TestArgs pins the restic contract: every worker is cacheless, locking ops
// retry the lock, and forget interpolates keepDays.
func TestArgs(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{noCache, "init"}, InitArgs())
	assert.Equal(t, []string{noCache, "cat", "config", "--no-lock"}, CatConfigArgs())
	assert.Equal(t, []string{noCache, "unlock"}, UnlockArgs())
	assert.Equal(t, []string{noCache, retryLock, "check"}, CheckArgs())
	assert.Equal(t,
		[]string{noCache, retryLock, "backup", "/data", "--host", "h1", "--tag", "rss2tg"},
		BackupArgs("h1", "rss2tg"),
	)
	assert.Equal(t,
		[]string{noCache, retryLock, "forget", "--tag", "rss2tg", "--keep-daily", "3"},
		ForgetArgs("rss2tg", 3),
	)
	assert.Equal(t,
		[]string{noCache, retryLock, "forget", "--keep-within", "30d"},
		SweepArgs(30),
	)
	assert.Equal(t, []string{noCache, retryLock, "prune"}, PruneArgs())
}
