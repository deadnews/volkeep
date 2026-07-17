package restic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestParseRepoStats(t *testing.T) {
	t.Parallel()

	logs := `{"total_size":52428800,"total_uncompressed_size":60000000,"compression_ratio":1.2,"total_blob_count":10,"snapshots_count":42}
`
	stats, ok := ParseRepoStats(logs)
	require.True(t, ok)
	assert.Equal(t, uint64(52428800), stats.TotalSize)
	assert.Equal(t, uint64(60000000), stats.TotalUncompressedSize)
	assert.Equal(t, 42, stats.SnapshotsCount)

	empty, ok := ParseRepoStats(`{"total_size":0,"snapshots_count":0}` + "\n")
	assert.True(t, ok, "an empty repository is a valid stats result")
	assert.Equal(t, uint64(0), empty.TotalSize)

	_, ok = ParseRepoStats("scanning...\n")
	assert.False(t, ok)
}

func TestParseForget(t *testing.T) {
	t.Parallel()

	logs := `[{"tags":["vol1"],"host":"h1","paths":["/data"],` +
		`"keep":[{"time":"2026-07-16T03:00:00Z","tree":"aa","id":"s1"},{"time":"2026-07-15T03:00:00Z","tree":"bb","id":"s2"}],` +
		`"remove":[{"time":"2026-07-10T03:00:00Z","tree":"cc","id":"s3"}],` +
		`"reasons":[{"snapshot":{},"matches":["daily snapshot"]}]}]` + "\n"
	c, ok := ParseForget(logs)
	require.True(t, ok)
	assert.Equal(t, 2, c.Kept)
	assert.Equal(t, 1, c.Removed)

	none, ok := ParseForget(`[{"tags":["vol1"],"host":"h1","keep":null,"remove":null,"reasons":null}]` + "\n")
	require.True(t, ok, "null keep/remove lists are a valid forget result")
	assert.Equal(t, ForgetCounts{}, none)

	_, ok = ParseForget("Applying Policy: keep 3 daily snapshots\nkeep 3 snapshots:\n")
	assert.False(t, ok, "human output has no JSON groups")
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
