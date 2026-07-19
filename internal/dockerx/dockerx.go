// Package dockerx wraps the Docker SDK with the operations volkeep needs:
// list labelled containers, stop/start them, exec commands in them,
// and run ephemeral workers.
package dockerx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/strslice"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// Client wraps the Docker SDK client.
type Client struct{ api *client.Client }

// New connects using standard env vars (DOCKER_HOST etc.) with API negotiation.
func New() (*Client, error) {
	c, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("new client: %w", err)
	}
	return &Client{api: c}, nil
}

// Close releases the underlying HTTP client.
func (c *Client) Close() error {
	if err := c.api.Close(); err != nil {
		return fmt.Errorf("close client: %w", err)
	}
	return nil
}

// Container is the slice of container state volkeep cares about.
type Container struct {
	ID      string
	Name    string
	Running bool
	Labels  map[string]string
	Volumes []Volume
}

// Volume is a named-volume mount on a container; bind mounts are excluded.
type Volume struct {
	Name        string
	Destination string
}

// ListLabeled returns all containers carrying labelKey=true.
func (c *Client) ListLabeled(ctx context.Context, labelKey string) ([]Container, error) {
	args := filters.NewArgs(filters.Arg("label", labelKey+"=true"))
	raw, err := c.api.ContainerList(ctx, container.ListOptions{All: true, Filters: args})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	out := make([]Container, 0, len(raw))
	for i := range raw {
		r := &raw[i]
		name := ""
		if len(r.Names) > 0 {
			name = strings.TrimPrefix(r.Names[0], "/")
		}
		vols := make([]Volume, 0, len(r.Mounts))
		for j := range r.Mounts {
			m := &r.Mounts[j]
			if m.Type == mount.TypeVolume && m.Name != "" && !isAnonVolume(m.Name) {
				vols = append(vols, Volume{Name: m.Name, Destination: m.Destination})
			}
		}
		out = append(out, Container{
			ID:      r.ID,
			Name:    name,
			Running: r.State == "running",
			Labels:  r.Labels,
			Volumes: vols,
		})
	}
	return out, nil
}

// isAnonVolume reports whether name is a Docker anonymous volume (64-char hex).
func isAnonVolume(name string) bool {
	if len(name) != 64 {
		return false
	}
	for _, r := range name {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// Stop stops a running container by ID.
func (c *Client) Stop(ctx context.Context, id string) error {
	if err := c.api.ContainerStop(ctx, id, container.StopOptions{}); err != nil {
		return fmt.Errorf("stop %s: %w", id, err)
	}
	return nil
}

// Start starts a previously-stopped container.
func (c *Client) Start(ctx context.Context, id string) error {
	if err := c.api.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		return fmt.Errorf("start %s: %w", id, err)
	}
	return nil
}

// Exec runs argv inside a running container and waits for it to finish.
func (c *Client) Exec(ctx context.Context, id string, argv []string) (RunResult, error) {
	exec, err := c.api.ContainerExecCreate(ctx, id, container.ExecOptions{
		Cmd:          argv,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return RunResult{ExitCode: -1}, fmt.Errorf("exec create %s: %w", id, err)
	}

	resp, err := c.api.ContainerExecAttach(ctx, exec.ID, container.ExecAttachOptions{})
	if err != nil {
		return RunResult{ExitCode: -1}, fmt.Errorf("exec attach %s: %w", id, err)
	}
	defer resp.Close()

	var buf bytes.Buffer
	if _, err := stdcopy.StdCopy(&buf, &buf, resp.Reader); err != nil {
		return RunResult{ExitCode: -1, Logs: buf.String()}, fmt.Errorf("exec read %s: %w", id, err)
	}

	// Stream EOF can precede the daemon recording the exit code; poll until done.
	for {
		ins, err := c.api.ContainerExecInspect(ctx, exec.ID)
		if err != nil {
			return RunResult{ExitCode: -1, Logs: buf.String()}, fmt.Errorf("exec inspect %s: %w", id, err)
		}
		if !ins.Running {
			return RunResult{ExitCode: ins.ExitCode, Logs: buf.String()}, nil
		}
		select {
		case <-ctx.Done():
			return RunResult{ExitCode: -1, Logs: buf.String()}, fmt.Errorf("exec wait %s: %w", id, ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// RunSpec describes an ephemeral worker container.
type RunSpec struct {
	Name   string
	Image  string
	Args   []string
	Env    []string
	Mounts []mount.Mount
}

// RunResult holds the exit code and combined stdout+stderr of a finished worker.
type RunResult struct {
	ExitCode int
	Logs     string
}

// Run creates, starts, waits, and removes a worker container.
// The image must already be present locally; call [Client.Pull] first.
func (c *Client) Run(ctx context.Context, spec *RunSpec) (RunResult, error) {
	cfg := &container.Config{Image: spec.Image, Cmd: spec.Args, Env: spec.Env}
	hostCfg := &container.HostConfig{
		Mounts:      spec.Mounts,
		CapDrop:     strslice.StrSlice{"ALL"},
		CapAdd:      strslice.StrSlice{"DAC_READ_SEARCH"},
		SecurityOpt: []string{"no-new-privileges"},
	}
	if spec.Name != "" {
		// A crashed run can strand a same-named container.
		_ = c.api.ContainerRemove(ctx, spec.Name, container.RemoveOptions{Force: true})
	}
	resp, err := c.api.ContainerCreate(ctx, cfg, hostCfg, nil, nil, spec.Name)
	if err != nil {
		return RunResult{ExitCode: -1}, fmt.Errorf("create worker: %w", err)
	}
	id := resp.ID
	defer func() {
		rmCtx := context.WithoutCancel(ctx)
		// Graceful stop lets restic release its repo lock before force-removal.
		_ = c.api.ContainerStop(rmCtx, id, container.StopOptions{Signal: "SIGINT"})
		_ = c.api.ContainerRemove(rmCtx, id, container.RemoveOptions{Force: true})
	}()

	if err := c.api.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		return RunResult{ExitCode: -1}, fmt.Errorf("start worker: %w", err)
	}

	logs, logErr := c.collectLogs(ctx, id)

	statusCh, errCh := c.api.ContainerWait(ctx, id, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		return RunResult{ExitCode: -1, Logs: logs}, errors.Join(fmt.Errorf("wait worker: %w", err), logErr)
	case s := <-statusCh:
		if logErr != nil {
			slog.Warn("Failed to collect worker logs", "error", logErr)
		}
		return RunResult{ExitCode: int(s.StatusCode), Logs: logs}, nil
	case <-ctx.Done():
		return RunResult{ExitCode: -1, Logs: logs}, fmt.Errorf("wait worker: %w", ctx.Err())
	}
}

// HasImage reports whether the image is present locally.
func (c *Client) HasImage(ctx context.Context, ref string) bool {
	_, err := c.api.ImageInspect(ctx, ref)
	return err == nil
}

// Pull fetches an image, blocking until the transfer completes.
func (c *Client) Pull(ctx context.Context, ref string) error {
	r, err := c.api.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull %s: %w", ref, err)
	}
	defer func() { _ = r.Close() }()
	if _, err := io.Copy(io.Discard, r); err != nil {
		return fmt.Errorf("pull %s: drain: %w", ref, err)
	}
	return nil
}

func (c *Client) collectLogs(ctx context.Context, id string) (string, error) {
	r, err := c.api.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true, ShowStderr: true, Follow: true,
	})
	if err != nil {
		return "", fmt.Errorf("logs %s: %w", id, err)
	}
	defer func() { _ = r.Close() }()

	var buf bytes.Buffer
	if _, err := stdcopy.StdCopy(&buf, &buf, r); err != nil {
		return buf.String(), fmt.Errorf("read logs %s: %w", id, err)
	}
	return buf.String(), nil
}
