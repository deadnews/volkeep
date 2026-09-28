package main

import (
	"cmp"
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
	var hooked, plain []Group
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
		g := Group{
			Container:     c,
			Volumes:       vols,
			Exec:          spec.Exec,
			RetentionDays: cmp.Or(spec.RetentionDays, defaultRetention),
			Stop:          spec.Stop,
		}
		if g.hooked() {
			hooked = append(hooked, g)
		} else {
			plain = append(plain, g)
		}
	}

	// Hooked groups claim first, so a shared volume keeps its exec or stop.
	candidates := slices.Concat(hooked, plain)
	out := make([]Group, 0, len(candidates))
	claimed := make(map[string]string)
	for i := range candidates {
		g := &candidates[i]
		kept := make([]string, 0, len(g.Volumes))
		for _, name := range g.Volumes {
			if owner, dup := claimed[name]; dup {
				if g.hooked() {
					slog.Warn("Shared volume backed up without this container's exec and stop", "volume", name, "container", g.Container.Name, "claimed_by", owner)
				}
				continue
			}
			claimed[name] = g.Container.Name
			kept = append(kept, name)
		}
		if len(kept) == 0 {
			slog.Info("Skipping container: no volumes to back up", "container", g.Container.Name)
			continue
		}
		g.Volumes = kept
		out = append(out, *g)
	}
	return out
}

func (g *Group) hooked() bool { return len(g.Exec) > 0 || g.Stop }

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
