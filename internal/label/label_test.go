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
		"volkeep.exec-pre":       "pg_dump -Fc -f /dump/db.dump app",
		"volkeep.stop":           "true",
		"volkeep.retention-days": "3",
	})
	require.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, Spec{
		Volumes:       []string{"data", "cache"},
		Exec:          []string{"pg_dump", "-Fc", "-f", "/dump/db.dump", "app"},
		Stop:          true,
		RetentionDays: 3,
	}, s)
}

func TestParse_ExecRequiresVolumes(t *testing.T) {
	t.Parallel()

	_, _, err := Parse(map[string]string{
		"volkeep.enable":   "true",
		"volkeep.exec-pre": "pg_dump -f /dump/db.dump app",
	})
	require.Error(t, err)
}

func TestParse_InvalidExec(t *testing.T) {
	t.Parallel()

	for _, v := range []string{"  ", "sh -c 'unterminated"} {
		_, _, err := Parse(map[string]string{
			"volkeep.enable":   "true",
			"volkeep.volumes":  "dump",
			"volkeep.exec-pre": v,
		})
		require.Error(t, err, "value %q should be rejected", v)
	}
}

func TestSplitCommand(t *testing.T) {
	t.Parallel()

	cases := map[string][]string{
		"pg_dump -U app app":                      {"pg_dump", "-U", "app", "app"},
		"/bin/sh -c 'pg_dump app > /dump/db.sql'": {"/bin/sh", "-c", "pg_dump app > /dump/db.sql"},
		`sh -c "echo 'hi'"`:                       {"sh", "-c", "echo 'hi'"},
		"cmd ''":                                  {"cmd", ""},
		"  spaced\targs  ":                        {"spaced", "args"},
		"":                                        nil,
	}
	for in, want := range cases {
		got, err := splitCommand(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}

	_, err := splitCommand("sh -c 'oops")
	require.Error(t, err, "unterminated quote")
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
