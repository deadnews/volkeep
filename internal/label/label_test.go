package label

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_Disabled(t *testing.T) {
	t.Parallel()

	cases := []map[string]string{
		nil,
		{},
		{"unrelated.label": "x"},
		{"volkeep.enable": "false"},
		{"volkeep.enable": ""},
	}
	for _, in := range cases {
		_, enabled, err := Parse(in)
		require.NoError(t, err)
		assert.False(t, enabled)
	}
}

func TestParse_Minimal(t *testing.T) {
	t.Parallel()

	s, enabled, err := Parse(map[string]string{"volkeep.enable": "true"})
	require.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, Spec{}, s)
}

func TestParse_Full(t *testing.T) {
	t.Parallel()

	s, enabled, err := Parse(map[string]string{
		"volkeep.enable":         "true",
		"volkeep.volumes":        "data, cache ,",
		"volkeep.stop":           "true",
		"volkeep.retention-days": "3",
	})
	require.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, Spec{
		Volumes:       []string{"data", "cache"},
		Stop:          true,
		RetentionDays: 3,
	}, s)
}

func TestParse_InvalidStop(t *testing.T) {
	t.Parallel()

	_, _, err := Parse(map[string]string{
		"volkeep.enable": "true",
		"volkeep.stop":   "yes",
	})
	require.Error(t, err)
}

func TestParse_InvalidRetention(t *testing.T) {
	t.Parallel()

	for _, v := range []string{"0", "-1", "abc"} {
		_, _, err := Parse(map[string]string{
			"volkeep.enable":         "true",
			"volkeep.retention-days": v,
		})
		require.Error(t, err, "value %q should be rejected", v)
	}
}
