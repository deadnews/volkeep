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
		slog.Warn("Pull failed; using local image", "image", d.cfg.ResticImage, "error", err)
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
	slog.Info("Jitter delay", "for", delay)
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
		slog.Error("Discovery failed", "error", err)
		return
	}
	targets := discover(raw, d.cfg.RetentionDays)
	slog.Info("Backup pass starting", "targets", len(targets))

	succeeded := 0
	for _, g := range groupByContainer(targets) {
		if ctx.Err() != nil {
			return
		}
		succeeded += d.runGroup(ctx, g)
	}

	if succeeded > 0 && ctx.Err() == nil {
		d.prune(ctx)
	}
	if d.cfg.Check && ctx.Err() == nil {
		d.check(ctx)
	}
	slog.Info("Backup pass finished")
}

// check verifies repo integrity.
func (d *Daemon) check(ctx context.Context) {
	res, err := d.docker.Run(ctx, &dockerx.RunSpec{
		Name:   workerName,
		Image:  d.cfg.ResticImage,
		Args:   restic.CheckArgs(),
		Env:    d.env,
		Mounts: d.repoMount(),
	})
	if err != nil || res.ExitCode != 0 {
		slog.Error("Repository check failed", "exit", res.ExitCode, "error", err, "logs", res.Logs)
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
	probe, err := d.docker.Run(ctx, &dockerx.RunSpec{
		Name:   workerName,
		Image:  d.cfg.ResticImage,
		Args:   restic.CatConfigArgs(),
		Env:    d.env,
		Mounts: d.repoMount(),
	})
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
	res, err := d.docker.Run(ctx, &dockerx.RunSpec{
		Name:   workerName,
		Image:  d.cfg.ResticImage,
		Args:   restic.InitArgs(),
		Env:    d.env,
		Mounts: d.repoMount(),
	})
	if err != nil {
		return fmt.Errorf("init repo: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("restic init failed (exit %d): %s", res.ExitCode, res.Logs)
	}
	return nil
}

// runGroup stops/restarts once per batch; returns successful backup count.
// forget runs post-restart to minimize downtime.
func (d *Daemon) runGroup(ctx context.Context, group []Target) int {
	if len(group) == 0 {
		return 0
	}
	head := group[0]
	if len(head.Exec) > 0 && !d.execHook(ctx, &head) {
		return 0
	}
	// Pre-stopped containers must stay stopped after the pass.
	shouldStop := head.Stop && head.Container.Running
	if shouldStop {
		slog.Info("Stopping container", "container", head.Container.Name)
		if err := d.docker.Stop(ctx, head.Container.ID); err != nil {
			slog.Error("Stop failed; skipping group", "container", head.Container.Name, "error", err)
			return 0
		}
	}

	succeeded := make([]*Target, 0, len(group))
	for i := range group {
		if d.backupOne(ctx, &group[i]) {
			succeeded = append(succeeded, &group[i])
		}
	}

	if shouldStop {
		// Restart even on shutdown, or a SIGTERM mid-pass strands the container.
		startCtx := context.WithoutCancel(ctx)
		if err := d.docker.Start(startCtx, head.Container.ID); err != nil {
			slog.Error("Restart failed", "container", head.Container.Name, "error", err)
		}
	}

	if ctx.Err() == nil {
		for _, t := range succeeded {
			d.forget(ctx, t)
		}
	}
	return len(succeeded)
}

// execHook runs the pre-backup command and reports whether the group can proceed.
func (d *Daemon) execHook(ctx context.Context, t *Target) bool {
	if !t.Container.Running {
		slog.Error("Exec skipped: container not running; skipping group", "container", t.Container.Name)
		return false
	}
	start := time.Now()
	res, err := d.docker.Exec(ctx, t.Container.ID, t.Exec)
	if err != nil || res.ExitCode != 0 {
		slog.Error("Exec failed; skipping group",
			"container", t.Container.Name,
			"exit", res.ExitCode, "error", err, "logs", res.Logs,
		)
		return false
	}
	slog.Info("Exec finished",
		"container", t.Container.Name,
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return true
}

func (d *Daemon) backupOne(ctx context.Context, t *Target) bool {
	start := time.Now()
	res, err := d.docker.Run(ctx, &dockerx.RunSpec{
		Name:  workerName,
		Image: d.cfg.ResticImage,
		Args:  restic.BackupArgs(d.cfg.HostTag, t.Volume.Name),
		Env:   d.env,
		Mounts: append(d.repoMount(), mount.Mount{
			Type:     mount.TypeVolume,
			Source:   t.Volume.Name,
			Target:   "/data",
			ReadOnly: true,
		}),
	})
	dur := time.Since(start)
	switch {
	case err != nil || (res.ExitCode != 0 && res.ExitCode != restic.ExitBackupPartial):
		slog.Error("Backup failed",
			"volume", t.Volume.Name,
			"duration_ms", dur.Milliseconds(),
			"exit", res.ExitCode, "error", err, "logs", res.Logs,
		)
		return false
	case res.ExitCode == restic.ExitBackupPartial:
		slog.Warn("Backup completed with unreadable files",
			"volume", t.Volume.Name,
			"duration_ms", dur.Milliseconds(),
			"logs", res.Logs,
		)
	default:
		slog.Info("Backup finished",
			"volume", t.Volume.Name,
			"duration_ms", dur.Milliseconds(),
		)
	}
	return true
}

func (d *Daemon) forget(ctx context.Context, t *Target) {
	res, err := d.docker.Run(ctx, &dockerx.RunSpec{
		Name:   workerName,
		Image:  d.cfg.ResticImage,
		Args:   restic.ForgetArgs(t.Volume.Name, t.RetentionDays),
		Env:    d.env,
		Mounts: d.repoMount(),
	})
	if err != nil || res.ExitCode != 0 {
		slog.Error("Forget failed",
			"volume", t.Volume.Name, "exit", res.ExitCode, "error", err, "logs", res.Logs,
		)
		return
	}
	slog.Info("Forget finished", "volume", t.Volume.Name)
}

// prune removes data unreferenced after forgets.
func (d *Daemon) prune(ctx context.Context) {
	res, err := d.docker.Run(ctx, &dockerx.RunSpec{
		Name:   workerName,
		Image:  d.cfg.ResticImage,
		Args:   restic.PruneArgs(),
		Env:    d.env,
		Mounts: d.repoMount(),
	})
	if err != nil || res.ExitCode != 0 {
		slog.Error("Prune failed", "exit", res.ExitCode, "error", err, "logs", res.Logs)
		return
	}
	slog.Info("Prune finished")
}
