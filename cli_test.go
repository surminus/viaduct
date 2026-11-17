package viaduct

import (
	"os"
	"testing"
	"time"

	flag "github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFlags(t *testing.T) {
	t.Run("applies given flags", func(t *testing.T) {
		var c CliFlags
		err := parseFlags(&c, []string{"--dry-run", "--quiet", "--resource-timeout", "2m", "--json"})
		require.NoError(t, err)

		assert.True(t, c.DryRun)
		assert.True(t, c.Quiet)
		assert.True(t, c.JSON)
		assert.Equal(t, 2*time.Minute, c.ResourceTimeout)
		assert.False(t, c.Silent)
	})

	t.Run("keeps values set before parsing", func(t *testing.T) {
		c := CliFlags{DryRun: true, ResourceTimeout: time.Minute}
		require.NoError(t, parseFlags(&c, nil))

		assert.True(t, c.DryRun)
		assert.Equal(t, time.Minute, c.ResourceTimeout)
	})

	t.Run("a given flag overrides a value set before parsing", func(t *testing.T) {
		c := CliFlags{DryRun: true}
		require.NoError(t, parseFlags(&c, []string{"--dry-run=false"}))

		assert.False(t, c.DryRun)
	})

	t.Run("unknown flags are an error", func(t *testing.T) {
		var c CliFlags
		err := parseFlags(&c, []string{"--dry-rnu"})
		assert.ErrorContains(t, err, "unknown flag: --dry-rnu")
	})

	t.Run("help", func(t *testing.T) {
		var c CliFlags
		err := parseFlags(&c, []string{"--help"})
		assert.ErrorIs(t, err, flag.ErrHelp)
	})

	t.Run("silent and quiet together", func(t *testing.T) {
		var c CliFlags
		err := parseFlags(&c, []string{"--silent", "--quiet"})
		assert.EqualError(t, err, "cannot use --silent and --quiet together")
	})

	t.Run("environment", func(t *testing.T) {
		t.Setenv("VIADUCT_STDOUT", "true")
		t.Setenv("VIADUCT_RESOURCE_TIMEOUT", "30s")

		var c CliFlags
		require.NoError(t, parseFlags(&c, nil))

		assert.True(t, c.Stdout)
		assert.Equal(t, 30*time.Second, c.ResourceTimeout)
	})

	t.Run("a flag overrides the environment", func(t *testing.T) {
		t.Setenv("VIADUCT_RESOURCE_TIMEOUT", "30s")

		var c CliFlags
		require.NoError(t, parseFlags(&c, []string{"--resource-timeout", "1m"}))

		assert.Equal(t, time.Minute, c.ResourceTimeout)
	})

	t.Run("usage names the environment variables", func(t *testing.T) {
		usage := newFlagSet(&CliFlags{}).FlagUsages()

		assert.Contains(t, usage, "VIADUCT_RESOURCE_TIMEOUT")
		assert.Contains(t, usage, "VIADUCT_STDOUT")
	})

	t.Run("a bad environment value is an error", func(t *testing.T) {
		t.Setenv("VIADUCT_RESOURCE_TIMEOUT", "soon")

		var c CliFlags
		assert.Error(t, parseFlags(&c, nil))
	})
}

func TestLoadAttributes(t *testing.T) {
	a, err := LoadAttributes()
	require.NoError(t, err)

	b, err := LoadAttributes()
	require.NoError(t, err)

	assert.Same(t, a, b, "every call shares one value")
	assert.Same(t, a, Attributes())

	assert.NotEmpty(t, a.User.Username)
	assert.NotNil(t, a.Custom)
}

func TestLoadAttributesFromRelease(t *testing.T) {
	a, err := loadAttributes("test/fixtures/os-release_ubuntu")
	require.NoError(t, err)

	assert.Equal(t, "ubuntu", a.Platform.ID)
	assert.Equal(t, a.User, a.runuser)
}

func TestTmpDir(t *testing.T) {
	require.NoError(t, removeTmpDir())

	dir, err := TmpDir()
	require.NoError(t, err)
	assert.DirExists(t, dir)

	again, err := TmpDir()
	require.NoError(t, err)
	assert.Equal(t, dir, again, "the directory is reused within a run")

	file, err := TmpFile("foo")
	require.NoError(t, err)
	assert.Equal(t, dir+"/foo", file)

	require.NoError(t, removeTmpDir())
	assert.NoDirExists(t, dir)

	next, err := TmpDir()
	require.NoError(t, err)
	assert.DirExists(t, next, "a new directory is created after removal")

	require.NoError(t, removeTmpDir())
	_, err = os.Stat(next)
	assert.True(t, os.IsNotExist(err))
}
