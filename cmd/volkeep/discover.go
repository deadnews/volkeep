package main

import (
	"fmt"
	"log/slog"

	"github.com/deadnews/volkeep/internal/dockerx"
	"github.com/deadnews/volkeep/internal/label"
)

// Target is one volume slated for backup, resolved from a labeled container.
type Target struct {
	Container     dockerx.Container
	Volume        dockerx.Volume
	Exec          []string
	RetentionDays int
	Stop          bool
}

// discover resolves labeled containers into targets, skipping invalid ones.
func discover(containers []dockerx.Container, defaultRetention int) []Target {
	out := make([]Target, 0, len(containers))
	seen := make(map[string]bool) // a shared volume is backed up once
	for _, c := range containers {
		spec, enabled, err := label.Parse(c.Labels)
		if err != nil {
			slog.Error("Skipping container: invalid labels", "container", c.Name, "error", err)
			continue
		}
		if !enabled {
			continue
		}
		vols, err := pickVolumes(c, spec.Volumes)
		if err != nil {
			slog.Error("Skipping container: volume error", "container", c.Name, "error", err)
			continue
		}
		retention := defaultRetention
		if spec.RetentionDays > 0 {
			retention = spec.RetentionDays
		}
		for _, v := range vols {
			if seen[v.Name] {
				continue
			}
			seen[v.Name] = true
			out = append(out, Target{
				Container:     c,
				Volume:        v,
				Exec:          spec.Exec,
				RetentionDays: retention,
				Stop:          spec.Stop,
			})
		}
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

// groupByContainer batches targets by container so its stop or exec happens once.
func groupByContainer(targets []Target) [][]Target {
	var (
		out    [][]Target
		keys   []string
		groups = make(map[string][]Target)
	)
	for i := range targets {
		t := &targets[i]
		if !t.Stop && len(t.Exec) == 0 {
			out = append(out, []Target{*t})
			continue
		}
		key := t.Container.ID
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], *t)
	}
	for _, k := range keys {
		out = append(out, groups[k])
	}
	return out
}
