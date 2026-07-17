package main

import (
	"fmt"
	"log/slog"

	"github.com/deadnews/volkeep/internal/dockerx"
	"github.com/deadnews/volkeep/internal/label"
)

// Group is one container's backup batch: its volumes plus the optional
// exec and stop that wrap them.
type Group struct {
	Container     dockerx.Container
	Volumes       []dockerx.Volume
	Exec          []string
	RetentionDays int
	Stop          bool
}

// discover resolves labeled containers into backup groups, skipping invalid ones.
func discover(containers []dockerx.Container, defaultRetention int) []Group {
	out := make([]Group, 0, len(containers))
	seen := make(map[string]bool) // a shared volume is backed up once
	for _, c := range containers {
		spec, enabled, err := label.Parse(c.Labels)
		if err != nil {
			slog.Error("Failed to parse labels; skipping container", "container", c.Name, "error", err)
			continue
		}
		if !enabled {
			continue
		}
		vols, err := pickVolumes(c, spec.Volumes)
		if err != nil {
			slog.Error("Failed to resolve volumes; skipping container", "container", c.Name, "error", err)
			continue
		}
		var kept []dockerx.Volume
		for _, v := range vols {
			if !seen[v.Name] {
				seen[v.Name] = true
				kept = append(kept, v)
			}
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

func pickVolumes(c dockerx.Container, wanted []string) ([]dockerx.Volume, error) {
	if len(wanted) == 0 {
		return c.Volumes, nil
	}
	have := make(map[string]dockerx.Volume, len(c.Volumes))
	for _, v := range c.Volumes {
		have[v.Name] = v
	}
	out := make([]dockerx.Volume, 0, len(wanted))
	for _, name := range wanted {
		v, ok := have[name]
		if !ok {
			return nil, fmt.Errorf("volkeep.volumes references %q which is not mounted as a named volume", name)
		}
		out = append(out, v)
	}
	return out, nil
}
