// Package restic builds argv and env for restic worker containers.
package restic

import (
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

// InitArgs returns argv for `restic init`.
func InitArgs() []string { return []string{"init"} }

// CatConfigArgs returns argv for probing repo existence.
func CatConfigArgs() []string { return []string{"cat", "config", "--no-lock"} }

// UnlockArgs returns argv for removing stale repository locks.
func UnlockArgs() []string { return []string{"unlock"} }

// CheckArgs returns argv for a structural integrity check.
func CheckArgs() []string { return []string{"check"} }

// BackupArgs returns argv for backing up /data.
func BackupArgs(hostTag, tag string) []string {
	return []string{
		"--json", "--quiet",
		"backup", "/data",
		"--host", hostTag,
		"--tag", tag,
	}
}

// ForgetArgs returns argv for forgetting snapshots scoped to a tag.
func ForgetArgs(tag string, keepDays int) []string {
	return []string{"forget", "--tag", tag, "--keep-daily", strconv.Itoa(keepDays)}
}

// SweepArgs returns argv for forgetting snapshots older than maxAgeDays.
func SweepArgs(maxAgeDays int) []string {
	return []string{"forget", "--keep-within", strconv.Itoa(maxAgeDays) + "d"}
}

// PruneArgs returns argv for removing data unreferenced after forgets.
func PruneArgs() []string { return []string{"prune"} }

// StatsArgs returns argv for measuring on-disk repository size.
func StatsArgs() []string { return []string{"--json", "stats", "--mode", "raw-data"} }
