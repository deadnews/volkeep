// Package label parses volkeep.* container labels into a backup [Spec].
package label

import (
	"fmt"
	"strconv"
	"strings"
)

// Prefix is the namespace shared by every label volkeep reads.
const Prefix = "volkeep."

// Spec is the per-container backup configuration parsed from labels.
// Empty Volumes and zero RetentionDays mean "use the default".
type Spec struct {
	Volumes       []string
	Exec          []string
	Stop          bool
	RetentionDays int
}

// Parse returns the spec; enabled is false unless volkeep.enable="true".
func Parse(labels map[string]string) (Spec, bool, error) {
	if labels[Prefix+"enable"] != "true" {
		return Spec{}, false, nil
	}

	var s Spec

	if v := labels[Prefix+"volumes"]; v != "" {
		for name := range strings.SplitSeq(v, ",") {
			if name = strings.TrimSpace(name); name != "" {
				s.Volumes = append(s.Volumes, name)
			}
		}
	}

	if v := labels[Prefix+"exec"]; v != "" {
		argv, err := splitCommand(v)
		if err != nil {
			return Spec{}, false, fmt.Errorf("label %sexec: %w", Prefix, err)
		}
		if len(argv) == 0 {
			return Spec{}, false, fmt.Errorf("label %sexec: empty command", Prefix)
		}
		s.Exec = argv
	}

	if v := labels[Prefix+"stop"]; v != "" {
		stop, err := strconv.ParseBool(v)
		if err != nil {
			return Spec{}, false, fmt.Errorf("label %sstop: %w", Prefix, err)
		}
		s.Stop = stop
	}

	if v := labels[Prefix+"retention-days"]; v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return Spec{}, false, fmt.Errorf("label %sretention-days: must be positive int, got %q", Prefix, v)
		}
		s.RetentionDays = n
	}

	// Defaulting to all mounts would snapshot the live data the exec exists to avoid.
	if len(s.Exec) > 0 && len(s.Volumes) == 0 {
		return Spec{}, false, fmt.Errorf("label %sexec requires %svolumes", Prefix, Prefix)
	}

	return s, true, nil
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
