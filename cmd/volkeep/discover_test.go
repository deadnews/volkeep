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
	require.Len(t, got, 2)
	assert.Equal(t, "app_data", got[0].Volume.Name)
	assert.Equal(t, "app_cache", got[1].Volume.Name)
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
	assert.Equal(t, "app_data", got[0].Volume.Name)
}

func TestDiscover_SharedVolume(t *testing.T) {
	t.Parallel()

	containers := []dockerx.Container{
		{Name: "a", Labels: map[string]string{"volkeep.enable": "true"}, Volumes: []dockerx.Volume{{Name: "shared"}}},
		{Name: "b", Labels: map[string]string{"volkeep.enable": "true"}, Volumes: []dockerx.Volume{{Name: "shared"}}},
	}
	got := discover(containers, 7)
	require.Len(t, got, 1, "shared volume backed up once")
	assert.Equal(t, "a", got[0].Container.Name, "first owner wins")
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

func TestGroupByStop(t *testing.T) {
	t.Parallel()

	a := dockerx.Container{ID: "a", Name: "app"}
	b := dockerx.Container{ID: "b", Name: "static"}

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, groupByStop(nil))
	})

	t.Run("batches same container", func(t *testing.T) {
		t.Parallel()
		groups := groupByStop([]Target{
			{Container: a, Volume: dockerx.Volume{Name: "v1"}, Stop: true},
			{Container: a, Volume: dockerx.Volume{Name: "v2"}, Stop: true},
			{Container: b, Volume: dockerx.Volume{Name: "v3"}},
		})
		require.Len(t, groups, 2)
		assert.Equal(t, "v3", groups[0][0].Volume.Name, "no-stop group first")
		assert.Len(t, groups[1], 2, "v1 and v2 batched under container a")
	})

	t.Run("all no-stop", func(t *testing.T) {
		t.Parallel()
		groups := groupByStop([]Target{
			{Container: a, Volume: dockerx.Volume{Name: "v1"}},
			{Container: b, Volume: dockerx.Volume{Name: "v2"}},
		})
		require.Len(t, groups, 2)
		for _, g := range groups {
			assert.Len(t, g, 1)
		}
	})

	t.Run("all stop, distinct containers", func(t *testing.T) {
		t.Parallel()
		groups := groupByStop([]Target{
			{Container: a, Volume: dockerx.Volume{Name: "v1"}, Stop: true},
			{Container: b, Volume: dockerx.Volume{Name: "v2"}, Stop: true},
		})
		require.Len(t, groups, 2)
		for _, g := range groups {
			assert.Len(t, g, 1)
		}
	})
}
