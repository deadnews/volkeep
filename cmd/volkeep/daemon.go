package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"time"

	"github.com/docker/docker/api/types/mount"

	"github.com/deadnews/volkeep/internal/dockerx"
	"github.com/deadnews/volkeep/internal/label"
	"github.com/deadnews/volkeep/internal/restic"
)

// dockerClient is the subset of [dockerx.Client] the daemon depends on.
type dockerClient interface {
	Pull(ctx context.Context, ref string) error
	HasImage(ctx context.Context, ref string) bool
	ListLabeled(ctx context.Context, labelKey string) ([]dockerx.Container, error)
	Run(ctx context.Context, spec *dockerx.RunSpec) (dockerx.RunResult, error)
	Exec(ctx context.Context, id string, argv []string) (dockerx.RunResult, error)
	Stop(ctx context.Context, id string) error
	Start(ctx context.Context, id string) error
}

// Daemon orchestrates one host's backup runs.
type Daemon struct {
	cfg    *Config
	docker dockerClient
	env    []string // precomputed restic env passed to every worker
	fire   chan struct{}
}

// NewDaemon constructs a Daemon.
func NewDaemon(cfg *Config, dx dockerClient) *Daemon {
	environ := os.Environ()
	env := restic.BaseEnv(cfg.ResticRepo, cfg.ResticPassword)
	env = append(env, restic.AwsEnv(environ)...)
	env = append(env, restic.RcloneEnv(environ)...)

	return &Daemon{
		cfg:    cfg,
		docker: dx,
		env:    env,
		fire:   make(chan struct{}, 1),
	}
}

// Trigger requests an out-of-schedule pass; non-blocking, coalesces.
func (d *Daemon) Trigger() {
	select {
	case d.fire <- struct{}{}:
	default:
	}
}

// Run blocks until ctx is cancelled, firing one pass per schedule tick or Trigger.
func (d *Daemon) Run(ctx context.Context) error {
	if err := d.docker.Pull(ctx, d.cfg.ResticImage); err != nil {
		if !d.docker.HasImage(ctx, d.cfg.ResticImage) {
			return fmt.Errorf("pull restic image: %w", err)
		}
		slog.Warn("Failed to pull image; using local", "image", d.cfg.ResticImage, "error", err)
	}
	if err := d.initRepo(ctx); err != nil {
		return err
	}
	for {
		next := d.cfg.NextFire(time.Now())
		slog.Info("Next backup scheduled", "at", next.Format(time.RFC3339))
		t := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			t.Stop()
			return nil
		case <-t.C:
			if !d.applyJitter(ctx) {
				return nil
			}
			d.runOnce(ctx)
		case <-d.fire:
			t.Stop()
			slog.Info("Manual trigger")
			d.runOnce(ctx)
		}
	}
}

// applyJitter sleeps [0, Jitter) before the pass; returns false on ctx cancel.
func (d *Daemon) applyJitter(ctx context.Context) bool {
	if d.cfg.Jitter <= 0 {
		return true
	}
	delay := time.Duration(rand.Int64N(int64(d.cfg.Jitter))) //nolint:gosec // jitter is non-security
	slog.Info("Jitter delay", "duration", delay)
	select {
	case <-ctx.Done():
		return false
	case <-time.After(delay):
		return true
	}
}

func (d *Daemon) runOnce(ctx context.Context) {
	raw, err := d.docker.ListLabeled(ctx, label.Prefix+"enable")
	if err != nil {
		slog.Error("Failed to discover containers", "error", err)
		return
	}
	groups := discover(raw, d.cfg.RetentionDays)
	slog.Info("Backup pass starting", "containers", len(groups))
	d.unlock(ctx)

	succeeded := 0
	for i := range groups {
		if ctx.Err() != nil {
			return
		}
		succeeded += d.runGroup(ctx, &groups[i])
	}

	if succeeded > 0 && ctx.Err() == nil {
		d.sweep(ctx, groups)
		d.prune(ctx)
	}
	if d.cfg.Check && ctx.Err() == nil {
		d.check(ctx)
	}
	slog.Info("Backup pass finished")
}

// workerSpec assembles the RunSpec shared by every restic worker.
func (d *Daemon) workerSpec(args []string, mounts ...mount.Mount) *dockerx.RunSpec {
	return &dockerx.RunSpec{
		Name:   workerName,
		Image:  d.cfg.ResticImage,
		Args:   args,
		Env:    d.env,
		Mounts: append(d.repoMount(), mounts...),
	}
}

// unlock removes locks stranded by workers that died uncleanly. Safe alongside
// a live operation: its lock refreshes every ~5 min and never turns stale.
func (d *Daemon) unlock(ctx context.Context) {
	res, err := d.docker.Run(ctx, d.workerSpec(restic.UnlockArgs()))
	if err != nil || res.ExitCode != 0 {
		slog.Error("Unlock failed",
			"exit", res.ExitCode, "error", err, "logs", res.Logs)
		return
	}
	if res.Logs != "" {
		slog.Warn("Removed stale repository locks", "logs", res.Logs)
	}
}

// check verifies repo integrity.
func (d *Daemon) check(ctx context.Context) {
	res, err := d.docker.Run(ctx, d.workerSpec(restic.CheckArgs()))
	if err != nil || res.ExitCode != 0 {
		slog.Error("Repository check failed",
			"exit", res.ExitCode, "error", err, "logs", res.Logs)
		return
	}
	slog.Info("Repository check passed")
}

func (d *Daemon) repoMount() []mount.Mount {
	if d.cfg.RepoVolume == "" {
		return nil
	}
	return []mount.Mount{{Type: mount.TypeVolume, Source: d.cfg.RepoVolume, Target: workerRepoPath}}
}

// initRepo initializes the repo when restic reports it missing.
func (d *Daemon) initRepo(ctx context.Context) error {
	probe, err := d.docker.Run(ctx, d.workerSpec(restic.CatConfigArgs()))
	if err != nil {
		return fmt.Errorf("probe repo: %w", err)
	}
	switch probe.ExitCode {
	case 0:
		slog.Info("Restic repository present")
		return nil
	case restic.ExitRepoMissing:
	default:
		return fmt.Errorf("probe repo failed (exit %d): %s", probe.ExitCode, probe.Logs)
	}

	slog.Info("Initializing restic repository")
	res, err := d.docker.Run(ctx, d.workerSpec(restic.InitArgs()))
	if err != nil {
		return fmt.Errorf("init repo: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("restic init failed (exit %d): %s", res.ExitCode, res.Logs)
	}
	return nil
}

// runGroup stops/restarts once per group; returns successful backup count.
// forget runs post-restart to minimize downtime.
func (d *Daemon) runGroup(ctx context.Context, g *Group) int {
	if len(g.Exec) > 0 && !d.execHook(ctx, g) {
		return 0
	}
	// Pre-stopped containers must stay stopped after the pass.
	shouldStop := g.Stop && g.Container.Running
	if shouldStop {
		slog.Info("Stopping container", "container", g.Container.Name)
		if err := d.docker.Stop(ctx, g.Container.ID); err != nil {
			slog.Error("Failed to stop container; skipping group", "container", g.Container.Name, "error", err)
			return 0
		}
	}

	succeeded := make([]string, 0, len(g.Volumes))
	for _, v := range g.Volumes {
		if d.backupOne(ctx, v.Name) {
			succeeded = append(succeeded, v.Name)
		}
	}

	if shouldStop {
		// Restart even on shutdown, or a SIGTERM mid-pass strands the container.
		startCtx := context.WithoutCancel(ctx)
		if err := d.docker.Start(startCtx, g.Container.ID); err != nil {
			slog.Error("Failed to restart container", "container", g.Container.Name, "error", err)
		}
	}

	if ctx.Err() == nil {
		for _, name := range succeeded {
			d.forget(ctx, name, g.RetentionDays)
		}
	}
	return len(succeeded)
}

// execHook runs the pre-backup command and reports whether the group can proceed.
func (d *Daemon) execHook(ctx context.Context, g *Group) bool {
	if !g.Container.Running {
		slog.Error("Exec skipped: container not running; skipping group", "container", g.Container.Name)
		return false
	}
	start := time.Now()
	res, err := d.docker.Exec(ctx, g.Container.ID, g.Exec)
	if err != nil || res.ExitCode != 0 {
		slog.Error("Exec failed; skipping group",
			"container", g.Container.Name,
			"exit", res.ExitCode, "error", err, "logs", res.Logs,
		)
		return false
	}
	slog.Info("Exec finished",
		"container", g.Container.Name,
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return true
}

func (d *Daemon) backupOne(ctx context.Context, volume string) bool {
	start := time.Now()
	res, err := d.docker.Run(ctx, d.workerSpec(
		restic.BackupArgs(d.cfg.HostTag, volume),
		mount.Mount{
			Type:     mount.TypeVolume,
			Source:   volume,
			Target:   "/data",
			ReadOnly: true,
		},
	))
	dur := time.Since(start)
	switch {
	case err != nil || (res.ExitCode != 0 && res.ExitCode != restic.ExitBackupPartial):
		slog.Error("Backup failed",
			"volume", volume,
			"duration_ms", dur.Milliseconds(),
			"exit", res.ExitCode, "error", err, "logs", res.Logs,
		)
		return false
	case res.ExitCode == restic.ExitBackupPartial:
		slog.Warn("Backup completed with unreadable files",
			"volume", volume,
			"duration_ms", dur.Milliseconds(),
			"logs", res.Logs,
		)
	default:
		slog.Info("Backup finished",
			"volume", volume,
			"duration_ms", dur.Milliseconds(),
		)
	}
	return true
}

func (d *Daemon) forget(ctx context.Context, volume string, keepDays int) {
	res, err := d.docker.Run(ctx, d.workerSpec(restic.ForgetArgs(volume, keepDays)))
	if err != nil || res.ExitCode != 0 {
		slog.Error("Forget failed",
			"volume", volume, "exit", res.ExitCode, "error", err, "logs", res.Logs,
		)
		return
	}
	slog.Info("Forget finished", "volume", volume)
}

// sweep forgets snapshots older than MaxAgeDays, aging out stale volumes.
func (d *Daemon) sweep(ctx context.Context, groups []Group) {
	if d.cfg.MaxAgeDays == 0 {
		return
	}
	// Retention reaching the cutoff would lose snapshots it means to keep.
	for i := range groups {
		if g := &groups[i]; g.RetentionDays >= d.cfg.MaxAgeDays {
			slog.Error("Sweep skipped: retention reaches max age",
				"container", g.Container.Name,
				"retention_days", g.RetentionDays, "max_age_days", d.cfg.MaxAgeDays,
			)
			return
		}
	}
	res, err := d.docker.Run(ctx, d.workerSpec(restic.SweepArgs(d.cfg.MaxAgeDays)))
	if err != nil || res.ExitCode != 0 {
		slog.Error("Sweep failed", "exit", res.ExitCode, "error", err, "logs", res.Logs)
		return
	}
	slog.Info("Sweep finished")
}

// prune removes data unreferenced after forgets.
func (d *Daemon) prune(ctx context.Context) {
	res, err := d.docker.Run(ctx, d.workerSpec(restic.PruneArgs()))
	if err != nil || res.ExitCode != 0 {
		slog.Error("Prune failed", "exit", res.ExitCode, "error", err, "logs", res.Logs)
		return
	}
	slog.Info("Prune finished")
}
