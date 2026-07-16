package restic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		[]string{noCache, retryLock, "--json", "--quiet", "backup", "/data", "--host", "h1", "--tag", "rss2tg"},
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

func TestParseBackupSummary(t *testing.T) {
	t.Parallel()

	logs := `{"message_type":"error","error":{"message":"read failed"},"during":"archival","item":"/data/x"}
{"message_type":"summary","files_new":1,"files_changed":0,"files_unmodified":0,"dirs_new":2,"dirs_changed":0,"dirs_unmodified":0,"data_blobs":1,"tree_blobs":3,"data_added":1049,"data_added_packed":582,"total_files_processed":1,"total_bytes_processed":30052,"total_duration":0.25,"snapshot_id":"40dc1520ff"}
`
	sum, ok := ParseBackupSummary(logs)
	require.True(t, ok)
	assert.Equal(t, uint64(1049), sum.DataAdded)
	assert.Equal(t, uint64(582), sum.DataAddedPacked)
	assert.Equal(t, uint64(30052), sum.TotalBytesProcessed)
	assert.Equal(t, "40dc1520ff", sum.SnapshotID)

	_, ok = ParseBackupSummary("repository 3300 opened\nsnapshot 40dc1520 saved\n")
	assert.False(t, ok, "human output has no JSON summary")
}

func TestPlainLogs(t *testing.T) {
	t.Parallel()

	logs := `{"message_type":"error","error":{"message":"lstat /data/x: permission denied"},"during":"archival","item":"/data/x"}
{"message_type":"exit_error","code":1,"message":"unable to create lock in backend"}
{"message_type":"summary","data_added":1}
plain line
`
	assert.Equal(t,
		"lstat /data/x: permission denied\nunable to create lock in backend\nplain line\n",
		PlainLogs(logs),
	)
	assert.Empty(t, PlainLogs(""))
}
