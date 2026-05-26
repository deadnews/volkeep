package restic

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnv_AsSlice(t *testing.T) {
	t.Parallel()

	e := Env{Repository: "s3:h/b", Password: "pw"}
	assert.Equal(t, []string{
		"RESTIC_REPOSITORY=s3:h/b",
		"RESTIC_PASSWORD=pw",
	}, e.AsSlice())

	e.AwsAccessKey = "id"
	e.AwsSecretKey = "secret"
	assert.Equal(t, []string{
		"RESTIC_REPOSITORY=s3:h/b",
		"RESTIC_PASSWORD=pw",
		"AWS_ACCESS_KEY_ID=id",
		"AWS_SECRET_ACCESS_KEY=secret",
	}, e.AsSlice())
}

func TestInitArgs(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"--no-cache", "init"}, InitArgs())
}

func TestCatConfigArgs(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"--no-cache", "cat", "config"}, CatConfigArgs())
}

func TestCheckArgs(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"--no-cache", "check"}, CheckArgs())
}

func TestRcloneEnv(t *testing.T) {
	t.Parallel()
	environ := []string{"PATH=/bin", "RCLONE_CONFIG_R_TYPE=s3", "HOME=/root", "RCLONE_CONFIG_R_ACCESS_KEY_ID=id"}
	assert.Equal(t, []string{"RCLONE_CONFIG_R_TYPE=s3", "RCLONE_CONFIG_R_ACCESS_KEY_ID=id"}, RcloneEnv(environ))
	assert.Nil(t, RcloneEnv([]string{"PATH=/bin"}))
}

func TestBackupArgs(t *testing.T) {
	t.Parallel()
	assert.Equal(t,
		[]string{"--no-cache", "backup", "/data", "--host", "h1", "--tag", "rss2tg"},
		BackupArgs("h1", "rss2tg"),
	)
}

func TestForgetArgs(t *testing.T) {
	t.Parallel()
	assert.Equal(t,
		[]string{"--no-cache", "forget", "--tag", "rss2tg", "--keep-daily", "3", "--prune"},
		ForgetArgs("rss2tg", 3),
	)
}
