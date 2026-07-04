package main

import (
	"testing"

	"github.com/deadnews/volkeep/internal/dockerx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscover_SinglePerContainer(t *testing.T) {
	t.Parallel()

	containers := []dockerx.Container{
		{
			ID:   "abc",
			Name: "rss2tg",
			Labels: map[string]string{
				"volkeep.enable":         "true",
				"volkeep.stop":           "true",
				"volkeep.retention-days": "3",
			},
			Volumes: []dockerx.Volume{{Name: "rss2tg_data", Destination: "/data"}},
		},
	}
	got := discover(containers, 7)
	require.Len(t, got, 1)
	assert.Equal(t, 3, got[0].RetentionDays)
	assert.True(t, got[0].Stop)
}

func TestDiscover_MultiVolume(t *testing.T) {
	t.Parallel()

	containers := []dockerx.Container{
		{
			ID:     "abc",
			Name:   "app",
			Labels: map[string]string{"volkeep.enable": "true"},
			Volumes: []dockerx.Volume{
				{Name: "app_data", Destination: "/data"},
				{Name: "app_cache", Destination: "/cache"},
			},
		},
	}
	got := discover(containers, 7)
	require.Len(t, got, 1)
	require.Len(t, got[0].Volumes, 2, "one group holds both volumes")
	assert.Equal(t, "app_data", got[0].Volumes[0].Name)
	assert.Equal(t, "app_cache", got[0].Volumes[1].Name)
}

func TestDiscover_VolumesWhitelist(t *testing.T) {
	t.Parallel()

	containers := []dockerx.Container{
		{
			Name: "app",
			Labels: map[string]string{
				"volkeep.enable":  "true",
				"volkeep.volumes": "app_data",
			},
			Volumes: []dockerx.Volume{
				{Name: "app_data", Destination: "/data"},
				{Name: "app_cache", Destination: "/cache"},
			},
		},
	}
	got := discover(containers, 7)
	require.Len(t, got, 1)
	require.Len(t, got[0].Volumes, 1)
	assert.Equal(t, "app_data", got[0].Volumes[0].Name)
}

func TestDiscover_SharedVolume(t *testing.T) {
	t.Parallel()

	containers := []dockerx.Container{
		{Name: "a", Labels: map[string]string{"volkeep.enable": "true"}, Volumes: []dockerx.Volume{{Name: "shared"}}},
		{
			Name:   "b",
			Labels: map[string]string{"volkeep.enable": "true"},
			Volumes: []dockerx.Volume{
				{Name: "shared"},
				{Name: "b_data"},
			},
		},
	}
	got := discover(containers, 7)
	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0].Container.Name, "first owner wins the shared volume")
	require.Len(t, got[1].Volumes, 1, "shared volume backed up once")
	assert.Equal(t, "b_data", got[1].Volumes[0].Name)
}

func TestDiscover_AllVolumesShared(t *testing.T) {
	t.Parallel()

	containers := []dockerx.Container{
		{Name: "a", Labels: map[string]string{"volkeep.enable": "true"}, Volumes: []dockerx.Volume{{Name: "shared"}}},
		{Name: "b", Labels: map[string]string{"volkeep.enable": "true"}, Volumes: []dockerx.Volume{{Name: "shared"}}},
	}
	got := discover(containers, 7)
	require.Len(t, got, 1, "a container left with no volumes yields no group")
	assert.Equal(t, "a", got[0].Container.Name)
}

func TestDiscover_MissingVolume(t *testing.T) {
	t.Parallel()

	containers := []dockerx.Container{
		{
			Name: "app",
			Labels: map[string]string{
				"volkeep.enable":  "true",
				"volkeep.volumes": "missing",
			},
			Volumes: []dockerx.Volume{{Name: "other"}},
		},
	}
	assert.Empty(t, discover(containers, 7), "misconfigured container is skipped, not fatal")
}

func TestDiscover_Exec(t *testing.T) {
	t.Parallel()

	containers := []dockerx.Container{
		{
			Name: "db",
			Labels: map[string]string{
				"volkeep.enable":  "true",
				"volkeep.exec":    "pg_dump -Fc -f /dump/db.dump app",
				"volkeep.volumes": "app_dump",
			},
			Volumes: []dockerx.Volume{
				{Name: "app_data", Destination: "/var/lib/postgresql"},
				{Name: "app_dump", Destination: "/dump"},
			},
		},
	}
	got := discover(containers, 7)
	require.Len(t, got, 1)
	require.Len(t, got[0].Volumes, 1)
	assert.Equal(t, "app_dump", got[0].Volumes[0].Name)
	assert.Equal(t, []string{"pg_dump", "-Fc", "-f", "/dump/db.dump", "app"}, got[0].Exec)
}

func TestDiscover_ExecWithoutVolumes(t *testing.T) {
	t.Parallel()

	containers := []dockerx.Container{
		{
			Name: "db",
			Labels: map[string]string{
				"volkeep.enable": "true",
				"volkeep.exec":   "pg_dump -Fc -f /dump/db.dump app",
			},
			Volumes: []dockerx.Volume{{Name: "app_data"}},
		},
	}
	assert.Empty(t, discover(containers, 7), "exec without an explicit whitelist is skipped, not fatal")
}
