package resources

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surminus/viaduct"
	"github.com/surminus/viaduct/viaducttest"
)

func newTestDirectory(t *testing.T, path string) *Directory {
	d := &Directory{Path: path}

	err := d.PreflightChecks(testCtx, testLogger)
	if err != nil {
		t.Fatal(err)
	}

	return d
}

func TestDirectory(t *testing.T) {
	t.Parallel()

	t.Run("create", func(t *testing.T) {
		t.Parallel()

		d := newTestDirectory(t, filepath.Join(t.TempDir(), "test_create_directory"))

		err := d.Run(testCtx, testLogger)
		assert.NoError(t, err)

		assert.Equal(t, true, viaduct.DirExists(d.Path))
		assertMode(t, d.Path, DefaultDirectoryPermissions)
	})

	t.Run("no recursive chown", func(t *testing.T) {
		t.Parallel()

		if os.Geteuid() == 0 {
			t.Skip("root walks a directory regardless of its permissions")
		}

		path := filepath.Join(t.TempDir(), "test_no_recursive_chown")
		// A path the ownership cannot be applied to stands in for the real
		// case, such as a read-only mount
		unreachable := filepath.Join(path, "unreachable")

		err := os.MkdirAll(unreachable, 0o755)
		if err != nil {
			t.Fatal(err)
		}

		err = os.Chmod(unreachable, 0o000)
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = os.Chmod(unreachable, 0o755) })

		recursive := newTestDirectory(t, path)
		assert.Error(t, recursive.Run(testCtx, testLogger))

		shallow := DirShallow(path)
		err = shallow.PreflightChecks(testCtx, testLogger)
		if err != nil {
			t.Fatal(err)
		}

		assert.NoError(t, shallow.Run(testCtx, testLogger))
		assert.Equal(t, true, viaduct.DirExists(path))
		assertMode(t, path, DefaultDirectoryPermissions)
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()

		d := newTestDirectory(t, filepath.Join(t.TempDir(), "test_delete_directory"))
		d.Delete = true

		err := os.MkdirAll(d.Path, 0o755)
		if err != nil {
			t.Fatal(err)
		}

		err = d.Run(testCtx, testLogger)
		assert.NoError(t, err)

		assert.Equal(t, false, viaduct.DirExists(d.Path))
	})
}

func TestDirectoryRun(t *testing.T) {
	t.Parallel()

	t.Run("create, then a noop", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)

		log, err := env.Apply(env.Context(), Dir("/srv/app"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.True(t, viaduct.DirExists(env.Path("/srv/app")))
		assertMode(t, env.Path("/srv/app"), DefaultDirectoryPermissions)

		log, err = env.Apply(env.Context(), Dir("/srv/app"))
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Empty(t, env.Runner.Commands())
	})

	t.Run("dry run creates nothing", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)

		log, err := env.Apply(env.DryRunContext(), Dir("/srv/app"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.False(t, viaduct.DirExists(env.Path("/srv/app")))
	})

	t.Run("dry run checks the mode of an existing directory", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		require.NoError(t, os.MkdirAll(env.Path("/srv/app"), 0o700))

		log, err := env.Apply(env.DryRunContext(), Dir("/srv/app"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"chmod"}, env.Recorder.Messages(viaduct.LevelChanged))
		assertMode(t, env.Path("/srv/app"), os.ModeDir|0o700)
	})

	t.Run("dry run reports a correct directory as a noop", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		require.NoError(t, os.MkdirAll(env.Path("/srv/app"), 0o755))

		log, err := env.Apply(env.DryRunContext(), Dir("/srv/app"))
		require.NoError(t, err)

		assert.False(t, log.Changed())
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.WriteFile("/srv/app/file", "content")

		log, err := env.Apply(env.Context(), &Directory{Path: "/srv/app", Delete: true})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.False(t, viaduct.DirExists(env.Path("/srv/app")))

		log, err = env.Apply(env.Context(), &Directory{Path: "/srv/app", Delete: true})
		require.NoError(t, err)

		assert.False(t, log.Changed())
	})

	t.Run("dry run delete leaves the directory", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.WriteFile("/srv/app/file", "content")

		log, err := env.Apply(env.DryRunContext(), &Directory{Path: "/srv/app", Delete: true})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, "content", env.ReadFile("/srv/app/file"))
	})
}
