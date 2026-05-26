package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_Defaults(t *testing.T) {
	t.Setenv("VOLKEEP_SCHEDULE", "03:00")
	t.Setenv("VOLKEEP_HOST", "host1")
	t.Setenv("VOLKEEP_REPO_VOLUME", "/var/backups/restic")
	t.Setenv("RESTIC_PASSWORD", "x")

	c, err := LoadConfig()
	require.NoError(t, err)
	assert.Equal(t, 3, c.Hour)
	assert.Equal(t, 0, c.Minute)
	assert.Equal(t, defaultRetentionDays, c.RetentionDays)
	assert.True(t, c.Check, "check defaults on")
	assert.Equal(t, defaultResticImage, c.ResticImage)
	assert.Equal(t, "/var/backups/restic", c.RepoVolume)
	assert.Equal(t, "/repo", c.ResticRepo)
	assert.Equal(t, "host1", c.HostTag)
}

func TestLoadConfig_Remote(t *testing.T) {
	t.Setenv("VOLKEEP_SCHEDULE", "23:59")
	t.Setenv("VOLKEEP_HOST", "host1")
	t.Setenv("RESTIC_REPOSITORY", "s3:minio.host/bucket")
	t.Setenv("RESTIC_PASSWORD", "x")

	c, err := LoadConfig()
	require.NoError(t, err)
	assert.Empty(t, c.RepoVolume)
	assert.Equal(t, "s3:minio.host/bucket", c.ResticRepo)
}

func TestLoadConfig_Errors(t *testing.T) {
	allKeys := []string{
		"VOLKEEP_SCHEDULE", "VOLKEEP_HOST", "VOLKEEP_REPO_VOLUME",
		"RESTIC_REPOSITORY", "RESTIC_PASSWORD",
		"VOLKEEP_RETENTION_DAYS", "VOLKEEP_JITTER", "VOLKEEP_CHECK",
	}
	cases := map[string]map[string]string{
		"missing schedule": {"VOLKEEP_HOST": "h", "RESTIC_PASSWORD": "x", "RESTIC_REPOSITORY": "s3:h/b"},
		"missing host":     {"VOLKEEP_SCHEDULE": "03:00", "RESTIC_PASSWORD": "x", "RESTIC_REPOSITORY": "s3:h/b"},
		"missing password": {"VOLKEEP_SCHEDULE": "03:00", "VOLKEEP_HOST": "h", "RESTIC_REPOSITORY": "s3:h/b"},
		"missing repo":     {"VOLKEEP_SCHEDULE": "03:00", "VOLKEEP_HOST": "h", "RESTIC_PASSWORD": "x"},
		"bad schedule":     {"VOLKEEP_SCHEDULE": "25:00", "VOLKEEP_HOST": "h", "RESTIC_PASSWORD": "x", "RESTIC_REPOSITORY": "s3:h/b"},
		"both repos set":   {"VOLKEEP_SCHEDULE": "03:00", "VOLKEEP_HOST": "h", "RESTIC_PASSWORD": "x", "VOLKEEP_REPO_VOLUME": "v", "RESTIC_REPOSITORY": "s3:h/b"},
		"bad retention":    {"VOLKEEP_SCHEDULE": "03:00", "VOLKEEP_HOST": "h", "RESTIC_PASSWORD": "x", "RESTIC_REPOSITORY": "s3:h/b", "VOLKEEP_RETENTION_DAYS": "0"},
		"bad jitter":       {"VOLKEEP_SCHEDULE": "03:00", "VOLKEEP_HOST": "h", "RESTIC_PASSWORD": "x", "RESTIC_REPOSITORY": "s3:h/b", "VOLKEEP_JITTER": "nope"},
		"bad check":        {"VOLKEEP_SCHEDULE": "03:00", "VOLKEEP_HOST": "h", "RESTIC_PASSWORD": "x", "RESTIC_REPOSITORY": "s3:h/b", "VOLKEEP_CHECK": "nope"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			for _, k := range allKeys {
				t.Setenv(k, "")
			}
			for k, v := range env {
				t.Setenv(k, v)
			}
			_, err := LoadConfig()
			require.Error(t, err)
		})
	}
}

func TestLoadConfig_LocalVolume(t *testing.T) {
	t.Setenv("VOLKEEP_SCHEDULE", "03:00")
	t.Setenv("VOLKEEP_HOST", "host1")
	t.Setenv("VOLKEEP_REPO_VOLUME", "myrepo")
	t.Setenv("RESTIC_PASSWORD", "x")

	c, err := LoadConfig()
	require.NoError(t, err)
	assert.Equal(t, "myrepo", c.RepoVolume)
	assert.Equal(t, "/repo", c.ResticRepo)
}

func TestParseHHMM(t *testing.T) {
	t.Parallel()

	ok := map[string][2]int{
		"00:00": {0, 0},
		"03:00": {3, 0},
		"09:05": {9, 5},
		"23:59": {23, 59},
	}
	for in, want := range ok {
		h, m, err := parseHHMM(in)
		require.NoError(t, err, in)
		assert.Equal(t, want[0], h, in)
		assert.Equal(t, want[1], m, in)
	}

	bad := []string{"", "03-00", "24:00", "03:60", "03:00:00", "ab:cd"}
	for _, in := range bad {
		_, _, err := parseHHMM(in)
		require.Error(t, err, in)
	}
}

func TestNextFire(t *testing.T) {
	t.Parallel()

	c := &Config{Hour: 3, Minute: 0}
	loc := time.UTC

	cases := []struct {
		now, want time.Time
	}{
		{
			now:  time.Date(2026, 5, 26, 2, 0, 0, 0, loc),
			want: time.Date(2026, 5, 26, 3, 0, 0, 0, loc),
		},
		{
			now:  time.Date(2026, 5, 26, 3, 0, 0, 0, loc),
			want: time.Date(2026, 5, 27, 3, 0, 0, 0, loc),
		},
		{
			now:  time.Date(2026, 5, 26, 12, 0, 0, 0, loc),
			want: time.Date(2026, 5, 27, 3, 0, 0, 0, loc),
		},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, c.NextFire(tc.now))
	}
}
