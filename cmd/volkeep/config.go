package main

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	defaultResticImage   = "restic/restic"
	defaultRetentionDays = 5
	workerRepoPath       = "/repo"
)

// Config holds daemon settings sourced from environment variables.
type Config struct {
	Hour           int
	Minute         int
	Jitter         time.Duration
	RetentionDays  int
	Check          bool
	ResticImage    string
	HostTag        string
	RepoVolume     string
	ResticRepo     string
	ResticPassword string
	AwsAccessKey   string
	AwsSecretKey   string
}

// LoadConfig reads required and optional env vars.
func LoadConfig() (*Config, error) {
	sched := os.Getenv("VOLKEEP_SCHEDULE")
	if sched == "" {
		return nil, errors.New("VOLKEEP_SCHEDULE is required (HH:MM)")
	}
	hour, minute, err := parseHHMM(sched)
	if err != nil {
		return nil, fmt.Errorf("VOLKEEP_SCHEDULE: %w", err)
	}

	password := os.Getenv("RESTIC_PASSWORD")
	if password == "" {
		return nil, errors.New("RESTIC_PASSWORD is required")
	}

	retention := defaultRetentionDays
	if v := os.Getenv("VOLKEEP_RETENTION_DAYS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("VOLKEEP_RETENTION_DAYS: must be positive int, got %q", v)
		}
		retention = n
	}

	var jitter time.Duration
	if v := os.Getenv("VOLKEEP_JITTER"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			return nil, fmt.Errorf("VOLKEEP_JITTER: must be non-negative duration, got %q", v)
		}
		jitter = d
	}

	hostTag := os.Getenv("VOLKEEP_HOST")
	if hostTag == "" {
		return nil, errors.New("VOLKEEP_HOST is required (snapshot --host tag)")
	}

	check := true
	if v := os.Getenv("VOLKEEP_CHECK"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("VOLKEEP_CHECK: must be a bool, got %q", v)
		}
		check = b
	}

	repoVolume := os.Getenv("VOLKEEP_REPO_VOLUME")
	resticRepo := os.Getenv("RESTIC_REPOSITORY")
	switch {
	case repoVolume != "" && resticRepo != "":
		return nil, errors.New("set only one of VOLKEEP_REPO_VOLUME (local) or RESTIC_REPOSITORY (remote)")
	case repoVolume != "":
		resticRepo = workerRepoPath
	case resticRepo == "":
		return nil, errors.New("set VOLKEEP_REPO_VOLUME (local) or RESTIC_REPOSITORY (remote)")
	}

	return &Config{
		Hour:           hour,
		Minute:         minute,
		Jitter:         jitter,
		RetentionDays:  retention,
		Check:          check,
		ResticImage:    cmp.Or(os.Getenv("VOLKEEP_RESTIC_IMAGE"), defaultResticImage),
		HostTag:        hostTag,
		RepoVolume:     repoVolume,
		ResticRepo:     resticRepo,
		ResticPassword: password,
		AwsAccessKey:   os.Getenv("AWS_ACCESS_KEY_ID"),
		AwsSecretKey:   os.Getenv("AWS_SECRET_ACCESS_KEY"),
	}, nil
}

// NextFire returns the next time-of-day instant after now.
func (c *Config) NextFire(now time.Time) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), c.Hour, c.Minute, 0, 0, now.Location())
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

func parseHHMM(s string) (hour, minute int, err error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, 0, fmt.Errorf("expected HH:MM, got %q: %w", s, err)
	}
	return t.Hour(), t.Minute(), nil
}
