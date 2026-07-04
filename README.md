# volkeep

> Label-driven Docker volume backup daemon, powered by [restic](https://github.com/restic/restic)

[![GitHub: Release](https://img.shields.io/github/v/release/deadnews/volkeep?logo=github&logoColor=white)](https://github.com/deadnews/volkeep/releases/latest)
[![Docker: ghcr](https://img.shields.io/badge/docker-gray.svg?logo=docker&logoColor=white)](https://github.com/deadnews/volkeep/pkgs/container/volkeep)
[![CI: Main](https://img.shields.io/github/actions/workflow/status/deadnews/volkeep/main.yml?branch=main&logo=github&logoColor=white&label=main)](https://github.com/deadnews/volkeep)
[![CI: Coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/deadnews/volkeep/refs/heads/badges/coverage.json)](https://github.com/deadnews/volkeep)

Containers opt in via labels. At the scheduled time the daemon backs up their
named volumes through ephemeral `restic` workers, optionally stopping the
container for the duration, then prunes old snapshots. Backups land in a restic
repository: a local Docker volume, S3, or an rclone remote.

## Service labels

| Label                    | Default          | Purpose                                 |
| ------------------------ | ---------------- | --------------------------------------- |
| `volkeep.enable`         | required         | `true` to opt this container in         |
| `volkeep.stop`           | `false`          | Stop the container during backup        |
| `volkeep.exec`           | —                | Pre-backup command run in the container |
| `volkeep.volumes`        | all named mounts | Comma-separated whitelist               |
| `volkeep.retention-days` | daemon default   | Daily snapshots to keep                 |

```yml
name: app

services:
  app:
    image: app
    volumes:
      - data:/data:rw
    labels:
      volkeep.enable: true
      volkeep.stop: true

volumes:
  data:
```

Bind mounts and anonymous volumes are skipped.
Snapshots are tagged with the volume name.

## Daemon configuration

| Env                      | Default         | Description                              |
| ------------------------ | --------------- | ---------------------------------------- |
| `VOLKEEP_SCHEDULE`       | required        | Daily fire time `HH:MM` (daemon TZ)      |
| `VOLKEEP_HOST`           | required        | Identifier for `restic snapshots --host` |
| `RESTIC_REPOSITORY`      | required        | Restic URI, or `volume:<name>` (local)   |
| `RESTIC_PASSWORD`        | required        | Restic repo password                     |
| `AWS_*`                  | —               | Forwarded to workers (S3 backends)       |
| `RCLONE_*`               | —               | Forwarded to workers (rclone backends)   |
| `VOLKEEP_RETENTION_DAYS` | `5`             | Daily snapshots to keep                  |
| `VOLKEEP_CHECK`          | `true`          | Verify repo integrity after each pass    |
| `VOLKEEP_JITTER`         | `0`             | Random pre-fire delay (e.g. `30m`)       |
| `VOLKEEP_RESTIC_IMAGE`   | `restic/restic` | Worker image                             |
| `DOCKER_HOST`            | local socket    | Override to reach a proxied daemon       |

`RESTIC_REPOSITORY` selects the repository:

- **Local** — `volume:<name>` uses a Docker named volume as the repo, backed by
  a bind mount or any driver via `driver_opts`.
- **Remote** — an S3 or rclone backend URI.

For `rclone` remotes, point `VOLKEEP_RESTIC_IMAGE` at an image bundling the
`rclone` binary (e.g. `tofran/restic-rclone`) and configure it with
`RCLONE_CONFIG_*`.

`RESTIC_PASSWORD` is fixed at repo init. Rotating it later locks you out of
existing snapshots. Use `restic key add` instead.

## Multi-host

By design, each host runs its own daemon and repository. To share a single S3
bucket, give each host a distinct prefix (`s3:s3.host.com/bucket/<host>`) and
set `VOLKEEP_JITTER` to spread concurrent fires.

## Manual trigger

Run a backup pass on demand:

```sh
docker kill -s SIGUSR1 volkeep
```

## Databases

A live database can be dumped instead of stopped: `volkeep.exec` runs a
command inside the container before its volumes are backed up, and
`volkeep.volumes` must whitelist the volume receiving the dump. A non-zero
exit skips the backup.

## Deploy

`volkeep` needs access to the Docker API. [`compose.dev.yml`](./compose.dev.yml)
wires it through a socket-proxy and shows the full stack. The snippets
below cover only `volkeep`'s own config.

Local:

```yml
name: volkeep

services:
  volkeep:
    image: ghcr.io/deadnews/volkeep
    container_name: volkeep
    environment:
      VOLKEEP_SCHEDULE: 03:00
      VOLKEEP_HOST: ${HOSTNAME:-web-1}
      RESTIC_REPOSITORY: volume:volkeep_backup
      RESTIC_PASSWORD: ${RESTIC_PASSWORD}

volumes:
  backup:
```

Remote:

```yml
name: volkeep

services:
  volkeep:
    image: ghcr.io/deadnews/volkeep
    container_name: volkeep
    environment:
      VOLKEEP_SCHEDULE: 03:00
      VOLKEEP_JITTER: 30m
      VOLKEEP_HOST: ${HOSTNAME:-web-1}
      RESTIC_REPOSITORY: s3:s3.host.com/bucket/${HOSTNAME:-web-1}
      RESTIC_PASSWORD: ${RESTIC_PASSWORD}
      AWS_ACCESS_KEY_ID: ${AWS_ACCESS_KEY_ID}
      AWS_SECRET_ACCESS_KEY: ${AWS_SECRET_ACCESS_KEY}
```

## Restore

Backups are stored in an ordinary `restic` repository. Drive it with any
`restic` command (`restore`, `mount`). See the [restic docs](https://restic.readthedocs.io/en/stable/050_restore.html).

Local:

```sh
alias RESTIC='docker run --rm \
  -e RESTIC_PASSWORD \
  -v volkeep_backup:/repo \
  restic/restic -r /repo'

RESTIC snapshots --tag app_data
RESTIC restore latest --tag app_data --target /tmp/out
```

Remote:

```sh
alias RESTIC='docker run --rm \
  -e RESTIC_PASSWORD \
  -e AWS_ACCESS_KEY_ID \
  -e AWS_SECRET_ACCESS_KEY \
  restic/restic -r s3:s3.host.com/bucket/web-1'

RESTIC snapshots --host web-1 --tag app_data
RESTIC restore latest --host web-1 --tag app_data --target /tmp/out
```
