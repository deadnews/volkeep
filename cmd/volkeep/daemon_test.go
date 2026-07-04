package main

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	"github.com/deadnews/volkeep/internal/dockerx"
	"github.com/deadnews/volkeep/internal/restic"
)

func SkipIfNoTestcontainers(t *testing.T) {
	t.Helper()
	if os.Getenv("TESTCONTAINERS") != "1" {
		t.Skip("Skipping integration test, set TESTCONTAINERS=1 to run it.")
	}
}

func setupDaemon(t *testing.T) (context.Context, *dockerx.Client, *Daemon) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	repoVol := "volkeep_test_" + strings.ReplaceAll(t.Name(), "/", "_")
	t.Cleanup(func() {
		_ = exec.CommandContext(context.Background(), "docker", "volume", "rm", "-f", repoVol).Run()
	})

	cfg := &Config{
		RetentionDays:  5,
		Check:          true,
		ResticImage:    defaultResticImage,
		HostTag:        "test-host",
		RepoVolume:     repoVol,
		ResticRepo:     workerRepoPath,
		ResticPassword: "test-pw",
	}

	dx, err := dockerx.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = dx.Close() })

	d := NewDaemon(cfg, dx)
	require.NoError(t, dx.Pull(ctx, cfg.ResticImage))
	require.NoError(t, d.initRepo(ctx))

	return ctx, dx, d
}

func snapshots(ctx context.Context, t *testing.T, d *Daemon) string {
	t.Helper()
	res, err := d.docker.Run(ctx, &dockerx.RunSpec{
		Image:  d.cfg.ResticImage,
		Args:   []string{"--no-cache", "snapshots", "--no-lock"},
		Env:    d.env,
		Mounts: d.repoMount(),
	})
	require.NoError(t, err)
	require.Zero(t, res.ExitCode, "snapshots probe failed: %s", res.Logs)
	return res.Logs
}

func TestDaemon_RunOnce(t *testing.T) {
	SkipIfNoTestcontainers(t)
	ctx, _, d := setupDaemon(t)

	app, err := testcontainers.Run(ctx, "busybox:musl",
		testcontainers.WithCmd("sh", "-c", "echo hello > /data/file.txt; trap 'exit 0' TERM; sleep 3600 & wait"),
		testcontainers.WithLabels(map[string]string{
			"volkeep.enable":         "true",
			"volkeep.stop":           "true",
			"volkeep.retention-days": "1",
		}),
		testcontainers.WithMounts(testcontainers.VolumeMount("volkeep_test_runonce", "/data")),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(app) })

	time.Sleep(500 * time.Millisecond)

	d.runOnce(ctx)

	logs := snapshots(ctx, t, d)
	assert.Contains(t, logs, "volkeep_test_runonce", "snapshot for our volume should be listed")
}

func TestDaemon_PreStoppedStaysDown(t *testing.T) {
	SkipIfNoTestcontainers(t)
	ctx, dx, d := setupDaemon(t)

	app, err := testcontainers.Run(ctx, "busybox:musl",
		testcontainers.WithCmd("sh", "-c", "echo hi > /data/file.txt; trap 'exit 0' TERM; sleep 3600 & wait"),
		testcontainers.WithLabels(map[string]string{
			"volkeep.enable": "true",
			"volkeep.stop":   "true",
		}),
		testcontainers.WithMounts(testcontainers.VolumeMount("volkeep_test_prestopped", "/data")),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(app) })

	time.Sleep(500 * time.Millisecond)

	require.NoError(t, dx.Stop(ctx, app.GetContainerID()))
	// Stop can return before the daemon's container listing reflects it;
	// wait so discovery sees the container as already stopped.
	require.Eventually(t, func() bool {
		state, err := app.State(ctx)
		return err == nil && !state.Running
	}, 10*time.Second, 100*time.Millisecond)

	d.runOnce(ctx)

	state, err := app.State(ctx)
	require.NoError(t, err)
	assert.False(t, state.Running, "pre-stopped container must remain stopped")
}

func TestDaemon_RestartsOnShutdown(t *testing.T) {
	SkipIfNoTestcontainers(t)
	ctx, _, d := setupDaemon(t)

	app, err := testcontainers.Run(ctx, "busybox:musl",
		testcontainers.WithCmd("sh", "-c", "echo hi > /data/file.txt; trap 'exit 0' TERM; sleep 3600 & wait"),
		testcontainers.WithLabels(map[string]string{
			"volkeep.enable": "true",
			"volkeep.stop":   "true",
		}),
		testcontainers.WithMounts(testcontainers.VolumeMount("volkeep_test_restart", "/data")),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(app) })

	time.Sleep(500 * time.Millisecond)

	group := &Group{
		Container: dockerx.Container{ID: app.GetContainerID(), Name: "app", Running: true},
		Volumes:   []dockerx.Volume{{Name: "volkeep_test_restart"}},
		Stop:      true,
	}

	// Cancel the pass once the daemon has stopped the container, simulating a
	// SIGTERM mid-backup; the restart must still bring it back up.
	passCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		for {
			if state, err := app.State(passCtx); err == nil && !state.Running {
				cancel()
				return
			}
			select {
			case <-passCtx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}()

	d.runGroup(passCtx, group)

	require.Eventually(t, func() bool {
		state, err := app.State(ctx)
		return err == nil && state.Running
	}, 30*time.Second, 200*time.Millisecond, "container the daemon stopped must be restarted on shutdown")
}

func TestDaemon_MultiVolume(t *testing.T) {
	SkipIfNoTestcontainers(t)
	ctx, _, d := setupDaemon(t)

	app, err := testcontainers.Run(ctx, "busybox:musl",
		testcontainers.WithCmd("sh", "-c", "echo a > /data1/a; echo b > /data2/b; trap 'exit 0' TERM; sleep 3600 & wait"),
		testcontainers.WithLabels(map[string]string{
			"volkeep.enable": "true",
			"volkeep.stop":   "true",
		}),
		testcontainers.WithMounts(
			testcontainers.VolumeMount("volkeep_test_mv_v1", "/data1"),
			testcontainers.VolumeMount("volkeep_test_mv_v2", "/data2"),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(app) })

	time.Sleep(500 * time.Millisecond)

	d.runOnce(ctx)

	logs := snapshots(ctx, t, d)
	assert.Contains(t, logs, "volkeep_test_mv_v1")
	assert.Contains(t, logs, "volkeep_test_mv_v2")
}

func TestDaemon_ExecDump(t *testing.T) {
	SkipIfNoTestcontainers(t)
	ctx, _, d := setupDaemon(t)

	app, err := testcontainers.Run(ctx, "busybox:musl",
		testcontainers.WithCmd("sh", "-c", "echo secret > /src/db; trap 'exit 0' TERM; sleep 3600 & wait"),
		testcontainers.WithLabels(map[string]string{
			"volkeep.enable":   "true",
			"volkeep.exec-pre": "sh -c 'cp /src/db /dump/db.dump'",
			"volkeep.volumes":  "volkeep_test_exec_dump",
		}),
		testcontainers.WithMounts(
			testcontainers.VolumeMount("volkeep_test_exec_src", "/src"),
			testcontainers.VolumeMount("volkeep_test_exec_dump", "/dump"),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(app) })

	time.Sleep(500 * time.Millisecond)

	d.runOnce(ctx)

	logs := snapshots(ctx, t, d)
	assert.Contains(t, logs, "volkeep_test_exec_dump", "the dump volume is snapshotted")
	assert.NotContains(t, logs, "volkeep_test_exec_src", "the live volume stays out of the backup")
}

// fakeDocker is an in-memory dockerClient for testing orchestration without Docker.
type fakeDocker struct {
	runFunc  func(spec *dockerx.RunSpec) (dockerx.RunResult, error)
	execFunc func(id string, argv []string) (dockerx.RunResult, error)
	runArgs  [][]string
	execed   []string
	stopped  []string
	started  []string
}

func (f *fakeDocker) Pull(context.Context, string) error    { return nil }
func (f *fakeDocker) HasImage(context.Context, string) bool { return true }

func (f *fakeDocker) ListLabeled(context.Context, string) ([]dockerx.Container, error) {
	return nil, nil
}

func (f *fakeDocker) Run(_ context.Context, spec *dockerx.RunSpec) (dockerx.RunResult, error) {
	f.runArgs = append(f.runArgs, spec.Args)
	if f.runFunc != nil {
		return f.runFunc(spec)
	}
	return dockerx.RunResult{}, nil
}

func (f *fakeDocker) Exec(_ context.Context, id string, argv []string) (dockerx.RunResult, error) {
	f.execed = append(f.execed, id)
	if f.execFunc != nil {
		return f.execFunc(id, argv)
	}
	return dockerx.RunResult{}, nil
}

func (f *fakeDocker) Stop(_ context.Context, id string) error {
	f.stopped = append(f.stopped, id)
	return nil
}

func (f *fakeDocker) Start(_ context.Context, id string) error {
	f.started = append(f.started, id)
	return nil
}

func (f *fakeDocker) ran(subcommand string) bool {
	return slices.ContainsFunc(f.runArgs, func(args []string) bool {
		return slices.Contains(args, subcommand)
	})
}

func newTestDaemon(fake *fakeDocker) *Daemon {
	return &Daemon{cfg: &Config{RetentionDays: 5}, docker: fake}
}

func backupExit(code int) func(*dockerx.RunSpec) (dockerx.RunResult, error) {
	return func(spec *dockerx.RunSpec) (dockerx.RunResult, error) {
		if slices.Contains(spec.Args, "backup") {
			return dockerx.RunResult{ExitCode: code}, nil
		}
		return dockerx.RunResult{}, nil
	}
}

func TestRunGroup_PartialBackupAppliesRetention(t *testing.T) {
	t.Parallel()

	fake := &fakeDocker{runFunc: backupExit(restic.ExitBackupPartial)}
	d := newTestDaemon(fake)
	group := &Group{Container: dockerx.Container{ID: "c1", Running: true}, Volumes: []dockerx.Volume{{Name: "v1"}}}

	assert.Equal(t, 1, d.runGroup(context.Background(), group), "partial backup counts as success")
	assert.True(t, fake.ran("forget"), "retention runs after a partial backup")
}

func TestRunGroup_FailedBackupSkipsRetention(t *testing.T) {
	t.Parallel()

	fake := &fakeDocker{runFunc: backupExit(1)}
	d := newTestDaemon(fake)
	group := &Group{Container: dockerx.Container{ID: "c1", Running: true}, Volumes: []dockerx.Volume{{Name: "v1"}}}

	assert.Equal(t, 0, d.runGroup(context.Background(), group), "failed backup is not counted")
	assert.False(t, fake.ran("forget"), "no retention for a failed backup")
}

func TestRunGroup_SkipsForgetOnCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	fake := &fakeDocker{runFunc: func(spec *dockerx.RunSpec) (dockerx.RunResult, error) {
		if slices.Contains(spec.Args, "backup") {
			cancel() // SIGTERM lands after the backup, before forget
		}
		return dockerx.RunResult{}, nil
	}}
	d := newTestDaemon(fake)
	group := &Group{Container: dockerx.Container{ID: "c1", Running: true}, Volumes: []dockerx.Volume{{Name: "v1"}}}

	assert.Equal(t, 1, d.runGroup(ctx, group), "the backup itself succeeded")
	assert.False(t, fake.ran("forget"), "retention is deferred on shutdown")
}

func TestRunGroup_RestartsStoppedContainerOnCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	fake := &fakeDocker{runFunc: func(spec *dockerx.RunSpec) (dockerx.RunResult, error) {
		if slices.Contains(spec.Args, "backup") {
			cancel()
		}
		return dockerx.RunResult{}, nil
	}}
	d := newTestDaemon(fake)
	group := &Group{Container: dockerx.Container{ID: "c1", Name: "app", Running: true}, Volumes: []dockerx.Volume{{Name: "v1"}}, Stop: true}

	d.runGroup(ctx, group)
	assert.Equal(t, []string{"c1"}, fake.stopped)
	assert.Equal(t, []string{"c1"}, fake.started, "a container the daemon stopped is restarted despite cancel")
}

func TestRunGroup_PreStoppedStaysDown(t *testing.T) {
	t.Parallel()

	fake := &fakeDocker{}
	d := newTestDaemon(fake)
	group := &Group{Container: dockerx.Container{ID: "c1", Running: false}, Volumes: []dockerx.Volume{{Name: "v1"}}, Stop: true}

	d.runGroup(context.Background(), group)
	assert.Empty(t, fake.stopped, "an already-stopped container is not stopped")
	assert.Empty(t, fake.started, "an already-stopped container is not restarted")
}

func TestSweep_DisabledByDefault(t *testing.T) {
	t.Parallel()

	fake := &fakeDocker{}
	d := newTestDaemon(fake)

	d.sweep(context.Background(), []Group{{RetentionDays: 5}})
	assert.False(t, fake.ran("--keep-within"))
}

func TestSweep_Runs(t *testing.T) {
	t.Parallel()

	fake := &fakeDocker{}
	d := &Daemon{cfg: &Config{RetentionDays: 5, MaxAgeDays: 30}, docker: fake}

	d.sweep(context.Background(), []Group{{RetentionDays: 5}})
	assert.True(t, fake.ran("--keep-within"))
}

func TestSweep_SkipsWhenRetentionReachesMaxAge(t *testing.T) {
	t.Parallel()

	fake := &fakeDocker{}
	d := &Daemon{cfg: &Config{RetentionDays: 5, MaxAgeDays: 30}, docker: fake}

	d.sweep(context.Background(), []Group{
		{RetentionDays: 5},
		{RetentionDays: 30, Container: dockerx.Container{Name: "app"}},
	})
	assert.False(t, fake.ran("--keep-within"), "a label retention reaching the cutoff blocks the sweep")
}

func TestRunGroup_ExecRunsOncePerGroup(t *testing.T) {
	t.Parallel()

	fake := &fakeDocker{}
	d := newTestDaemon(fake)
	group := &Group{
		Container: dockerx.Container{ID: "c1", Running: true},
		Volumes:   []dockerx.Volume{{Name: "v1"}, {Name: "v2"}},
		Exec:      []string{"pg_dump"},
	}

	assert.Equal(t, 2, d.runGroup(context.Background(), group))
	assert.Equal(t, []string{"c1"}, fake.execed, "exec runs once for the whole group")
	assert.True(t, fake.ran("backup"))
}

func TestRunGroup_ExecFailureSkipsGroup(t *testing.T) {
	t.Parallel()

	fake := &fakeDocker{execFunc: func(string, []string) (dockerx.RunResult, error) {
		return dockerx.RunResult{ExitCode: 1}, nil
	}}
	d := newTestDaemon(fake)
	group := &Group{
		Container: dockerx.Container{ID: "c1", Running: true},
		Volumes:   []dockerx.Volume{{Name: "v1"}},
		Exec:      []string{"pg_dump"},
		Stop:      true,
	}

	assert.Equal(t, 0, d.runGroup(context.Background(), group))
	assert.False(t, fake.ran("backup"), "a failed dump is never snapshotted")
	assert.Empty(t, fake.stopped, "exec gates the group before any stop")
}

func TestRunGroup_ExecNotRunningSkipsGroup(t *testing.T) {
	t.Parallel()

	fake := &fakeDocker{}
	d := newTestDaemon(fake)
	group := &Group{
		Container: dockerx.Container{ID: "c1", Running: false},
		Volumes:   []dockerx.Volume{{Name: "v1"}},
		Exec:      []string{"pg_dump"},
	}

	assert.Equal(t, 0, d.runGroup(context.Background(), group))
	assert.Empty(t, fake.execed, "no exec attempt on a stopped container")
	assert.False(t, fake.ran("backup"), "a stale dump is never snapshotted")
}
