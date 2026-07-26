// Package label parses volkeep.* container labels into a backup [Spec].
package label

import (
	"fmt"
	"strconv"
	"strings"
)

// The labels volkeep reads. A container opts in with EnableKey set to "true".
const (
	EnableKey        = "volkeep.enable"
	VolumesKey       = "volkeep.volumes"
	ExecPreKey       = "volkeep.exec-pre"
	StopKey          = "volkeep.stop"
	RetentionDaysKey = "volkeep.retention-days"
)

// Spec is the per-container backup configuration parsed from labels.
// Empty Volumes and zero RetentionDays mean "use the default".
type Spec struct {
	Volumes       []string
	Exec          []string
	Stop          bool
	RetentionDays int
}

// Parse returns the backup spec a container's labels describe.
func Parse(labels map[string]string) (Spec, error) {
	var s Spec

	if v := labels[VolumesKey]; v != "" {
		for name := range strings.SplitSeq(v, ",") {
			if name = strings.TrimSpace(name); name != "" {
				s.Volumes = append(s.Volumes, name)
			}
		}
	}

	if v := labels[ExecPreKey]; v != "" {
		argv, err := splitCommand(v)
		if err != nil {
			return Spec{}, fmt.Errorf("label %s: %w", ExecPreKey, err)
		}
		if len(argv) == 0 {
			return Spec{}, fmt.Errorf("label %s: empty command", ExecPreKey)
		}
		s.Exec = argv
	}

	if v := labels[StopKey]; v != "" {
		stop, err := strconv.ParseBool(v)
		if err != nil {
			return Spec{}, fmt.Errorf("label %s: %w", StopKey, err)
		}
		s.Stop = stop
	}

	if v := labels[RetentionDaysKey]; v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return Spec{}, fmt.Errorf("label %s: must be positive int, got %q", RetentionDaysKey, v)
		}
		s.RetentionDays = n
	}

	// Defaulting to all mounts would snapshot the live data the exec exists to avoid.
	if len(s.Exec) > 0 && len(s.Volumes) == 0 {
		return Spec{}, fmt.Errorf("label %s requires %s", ExecPreKey, VolumesKey)
	}

	return s, nil
}

// splitCommand splits a command line into argv.
func splitCommand(s string) ([]string, error) {
	var (
		args  []string
		cur   strings.Builder
		quote rune
		open  bool
	)
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			open = true
		case r == ' ' || r == '\t':
			if open {
				args = append(args, cur.String())
				cur.Reset()
				open = false
			}
		default:
			cur.WriteRune(r)
			open = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote", quote)
	}
	if open {
		args = append(args, cur.String())
	}
	return args, nil
}
