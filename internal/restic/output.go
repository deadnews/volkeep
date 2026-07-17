package restic

import (
	"encoding/json"
	"strings"
)

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

// RepoStats is the result object emitted by `stats --json`.
type RepoStats struct {
	TotalSize             uint64 `json:"total_size"`
	TotalUncompressedSize uint64 `json:"total_uncompressed_size"`
	SnapshotsCount        int    `json:"snapshots_count"`
}

// ParseRepoStats extracts the result from stats worker output.
func ParseRepoStats(logs string) (RepoStats, bool) {
	for line := range strings.Lines(logs) {
		if !strings.Contains(line, `"total_size"`) {
			continue
		}
		var s RepoStats
		if err := json.Unmarshal([]byte(line), &s); err == nil {
			return s, true
		}
	}
	return RepoStats{}, false
}

// jsonLogLine is the subset of `backup --json` message fields worth re-printing.
type jsonLogLine struct {
	MessageType string `json:"message_type"`
	Message     string `json:"message"`
	Error       struct {
		Message string `json:"message"`
	} `json:"error"`
}

// PlainLogs flattens `backup --json` output into plain error text.
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
			b.WriteString(m.Error.Message)
			b.WriteByte('\n')
		case "exit_error":
			b.WriteString(m.Message)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
