// Package restic builds argv and env for restic worker containers.
package restic

import (
	"encoding/json"
	"strconv"
	"strings"
)

const (
	// ExitBackupPartial is restic's exit code for a snapshot with unreadable files.
	ExitBackupPartial = 3
	// ExitRepoMissing is restic's exit code for a non-existent repository.
	ExitRepoMissing = 10
)

// BaseEnv returns the credentials forwarded to every worker;
// workers do not inherit daemon env.
func BaseEnv(repository, password string) []string {
	return []string{
		"RESTIC_REPOSITORY=" + repository,
		"RESTIC_PASSWORD=" + password,
	}
}

// AwsEnv returns the AWS_* entries from environ.
func AwsEnv(environ []string) []string { return prefixEnv(environ, "AWS_") }

// RcloneEnv returns the RCLONE_* entries from environ.
func RcloneEnv(environ []string) []string { return prefixEnv(environ, "RCLONE_") }

func prefixEnv(environ []string, prefix string) []string {
	var out []string
	for _, kv := range environ {
		if strings.HasPrefix(kv, prefix) {
			out = append(out, kv)
		}
	}
	return out
}

// Workers are ephemeral, so the restic cache cannot be reused between runs.
const noCache = "--no-cache"

// Wait out a prior worker's lock.
const retryLock = "--retry-lock=7s"

// InitArgs returns argv for `restic init`.
func InitArgs() []string { return []string{noCache, "init"} }

// CatConfigArgs returns argv for probing repo existence.
func CatConfigArgs() []string { return []string{noCache, "cat", "config", "--no-lock"} }

// UnlockArgs returns argv for removing stale repository locks.
func UnlockArgs() []string { return []string{noCache, "unlock"} }

// CheckArgs returns argv for a structural integrity check.
func CheckArgs() []string { return []string{noCache, retryLock, "check"} }

// BackupArgs returns argv for backing up /data. Workers run --json for the
// machine-readable summary and --quiet to drop the per-interval status lines.
func BackupArgs(hostTag, tag string) []string {
	return []string{
		noCache,
		retryLock,
		"--json", "--quiet",
		"backup", "/data",
		"--host", hostTag,
		"--tag", tag,
	}
}

// ForgetArgs returns argv for forgetting snapshots scoped to a tag.
func ForgetArgs(tag string, keepDays int) []string {
	return []string{
		noCache,
		retryLock,
		"forget",
		"--tag", tag,
		"--keep-daily", strconv.Itoa(keepDays),
	}
}

// SweepArgs returns argv for forgetting snapshots older than maxAgeDays.
func SweepArgs(maxAgeDays int) []string {
	return []string{
		noCache,
		retryLock,
		"forget",
		"--keep-within", strconv.Itoa(maxAgeDays) + "d",
	}
}

// PruneArgs returns argv for removing data unreferenced after forgets.
func PruneArgs() []string { return []string{noCache, retryLock, "prune"} }

// BackupSummary is the summary message emitted by `backup --json`.
type BackupSummary struct {
	MessageType         string `json:"message_type"`
	DataAdded           uint64 `json:"data_added"`
	DataAddedPacked     uint64 `json:"data_added_packed"`
	TotalBytesProcessed uint64 `json:"total_bytes_processed"`
	SnapshotID          string `json:"snapshot_id"`
}

// ParseBackupSummary extracts the summary message from backup worker output.
func ParseBackupSummary(logs string) (BackupSummary, bool) {
	for line := range strings.Lines(logs) {
		var s BackupSummary
		if err := json.Unmarshal([]byte(line), &s); err == nil && s.MessageType == "summary" {
			return s, true
		}
	}
	return BackupSummary{}, false
}

// jsonLogLine is the subset of `backup --json` message fields worth re-printing.
type jsonLogLine struct {
	MessageType string `json:"message_type"`
	Message     string `json:"message"` // exit_error
	Error       struct {
		Message string `json:"message"`
	} `json:"error"` // error
}

// PlainLogs flattens `backup --json` output into plain error text:
// error messages are unwrapped, other JSON is dropped, non-JSON passes through.
func PlainLogs(logs string) string {
	var b strings.Builder
	for line := range strings.Lines(logs) {
		var m jsonLogLine
		if err := json.Unmarshal([]byte(line), &m); err != nil || m.MessageType == "" {
			b.WriteString(line)
			continue
		}
		switch m.MessageType {
		case "error":
			b.WriteString(m.Error.Message + "\n")
		case "exit_error":
			b.WriteString(m.Message + "\n")
		}
	}
	return b.String()
}
