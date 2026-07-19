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

// WorkerEnv returns the env forwarded to every worker:
// repo credentials plus the AWS_* and RCLONE_* entries from environ.
func WorkerEnv(repository, password string, environ []string) []string {
	env := []string{
		"RESTIC_REPOSITORY=" + repository,
		"RESTIC_PASSWORD=" + password,
	}
	for _, kv := range environ {
		if strings.HasPrefix(kv, "AWS_") || strings.HasPrefix(kv, "RCLONE_") {
			env = append(env, kv)
		}
	}
	return env
}

// Argv builders, in pass order.

// InitArgs returns argv for `restic init`.
func InitArgs() []string { return []string{"init"} }

// CatConfigArgs returns argv for probing repo existence.
func CatConfigArgs() []string { return []string{"cat", "config", "--no-lock"} }

// UnlockArgs returns argv for removing stale repository locks.
func UnlockArgs() []string { return []string{"unlock"} }

// BackupArgs returns argv for backing up /data.
func BackupArgs(hostTag, tag string) []string {
	return []string{
		"backup", "/data",
		"--host", hostTag,
		"--tag", tag,
		"--json", "--quiet",
	}
}

// ForgetArgs returns argv for forgetting snapshots scoped to a tag.
func ForgetArgs(tag string, keepDays int) []string {
	return []string{"forget", "--tag", tag, "--keep-daily", strconv.Itoa(keepDays), "--quiet"}
}

// SweepArgs returns argv for forgetting snapshots older than maxAgeDays.
func SweepArgs(maxAgeDays int) []string {
	return []string{"forget", "--keep-within", strconv.Itoa(maxAgeDays) + "d", "--quiet"}
}

// PruneArgs returns argv for removing data unreferenced after forgets.
func PruneArgs() []string { return []string{"prune", "--quiet"} }

// CheckArgs returns argv for a structural integrity check.
func CheckArgs() []string { return []string{"check", "--quiet"} }

// StatsArgs returns argv for measuring on-disk repository size.
func StatsArgs() []string { return []string{"stats", "--mode", "raw-data", "--json"} }
