package main

import (
	"fmt"
	"log/slog"
	"slices"

	"github.com/deadnews/volkeep/internal/dockerx"
	"github.com/deadnews/volkeep/internal/label"
)

// Group is one container's backup batch: its volumes plus the optional
// exec and stop that wrap them.
type Group struct {
	Container     dockerx.Container
	Volumes       []string
	Exec          []string
	RetentionDays int
	Stop          bool
}

// discover resolves labeled containers into backup groups, skipping invalid ones.
func discover(containers []dockerx.Container, defaultRetention int) []Group {
	out := make([]Group, 0, len(containers))
	claimed := make(map[string]string)
	for _, c := range containers {
		spec, err := label.Parse(c.Labels)
		if err != nil {
			slog.Error("Failed to parse labels; skipping container", "container", c.Name, "error", err)
			continue
		}
		vols, err := pickVolumes(c, spec.Volumes)
		if err != nil {
			slog.Error("Failed to resolve volumes; skipping container", "container", c.Name, "error", err)
			continue
		}
		kept := make([]string, 0, len(vols))
		for _, name := range vols {
			if owner, dup := claimed[name]; dup {
				if len(spec.Exec) > 0 || spec.Stop {
					slog.Warn("Shared volume backed up without this container's exec and stop", "volume", name, "container", c.Name, "claimed_by", owner)
				}
				continue
			}
			claimed[name] = c.Name
			kept = append(kept, name)
		}
		if len(kept) == 0 {
			slog.Info("Skipping container: no volumes to back up", "container", c.Name)
			continue
		}
		retention := defaultRetention
		if spec.RetentionDays > 0 {
			retention = spec.RetentionDays
		}
		out = append(out, Group{
			Container:     c,
			Volumes:       kept,
			Exec:          spec.Exec,
			RetentionDays: retention,
			Stop:          spec.Stop,
		})
	}
	return out
}

func pickVolumes(c dockerx.Container, wanted []string) ([]string, error) {
	if len(wanted) == 0 {
		return c.Volumes, nil
	}
	for _, name := range wanted {
		if !slices.Contains(c.Volumes, name) {
			return nil, fmt.Errorf("label %s references %q which is not mounted as a named volume", label.VolumesKey, name)
		}
	}
	return wanted, nil
}
