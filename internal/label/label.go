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

	return s, true, nil
}
