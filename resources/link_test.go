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

func newTestLink(t *testing.T, source, dest string) *Link {
	d := &Link{Source: source, Path: dest}

	err := d.PreflightChecks(testCtx, testLogger)
	if err != nil {
		t.Fatal(err)
	}

	return d
}

// linkSource returns a file in a new temporary directory to link to.
func linkSource(t *testing.T) string {
	t.Helper()

	source := filepath.Join(t.TempDir(), "source.txt")
	require.NoError(t, os.WriteFile(source, []byte("source\n"), 0o644))

	return source
}

func TestLink(t *testing.T) {
	t.Parallel()

	t.Run("create", func(t *testing.T) {
		t.Parallel()

		source := linkSource(t)
		l := newTestLink(t, source, filepath.Join(filepath.Dir(source), "create.txt"))

		err := l.Run(testCtx, testLogger)
		assert.NoError(t, err)

		assert.Equal(t, true, viaduct.LinkExists(l.Path))
	})

	t.Run("create with missing parent dir", func(t *testing.T) {
		t.Parallel()

		source := linkSource(t)
		dir := filepath.Join(filepath.Dir(source), "createp")

		l := &Link{
			Source:             source,
			Path:               dir + "/nested/create.txt",
			CreateDirIfMissing: true,
		}
		if err := l.PreflightChecks(testCtx, testLogger); err != nil {
			t.Fatal(err)
		}

		assert.Equal(t, false, viaduct.DirExists(dir))

		err := l.Run(testCtx, testLogger)
		assert.NoError(t, err)

		assert.Equal(t, true, viaduct.LinkExists(l.Path))
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()

		source := linkSource(t)
		l := newTestLink(t, source, filepath.Join(filepath.Dir(source), "delete.txt"))
		l.Delete = true

		err := os.Symlink(viaduct.ExpandPath(l.Source), viaduct.ExpandPath(l.Path))
		if err != nil {
			t.Fatal(err)
		}

		err = l.Run(testCtx, testLogger)
		assert.NoError(t, err)

		assert.Equal(t, false, viaduct.LinkExists(l.Path))
	})
}

func TestLinkRun(t *testing.T) {
	t.Parallel()

	t.Run("create, then a noop", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)

		l := CreateLink("/usr/local/app", "/opt/app/current")
		l.CreateDirIfMissing = true

		log, err := env.Apply(env.Context(), l)
		require.NoError(t, err)

		assert.True(t, log.Changed())

		// The link points where it will on the host, not under the root
		target, err := os.Readlink(env.Path("/usr/local/app"))
		require.NoError(t, err)
		assert.Equal(t, "/opt/app/current", target)

		log, err = env.Apply(env.Context(), l)
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Empty(t, env.Runner.Commands())
	})

	t.Run("replaces a link to somewhere else", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		require.NoError(t, os.MkdirAll(env.Path("/usr/local"), 0o755))
		require.NoError(t, os.Symlink("/opt/app/old", env.Path("/usr/local/app")))

		log, err := env.Apply(env.Context(), CreateLink("/usr/local/app", "/opt/app/current"))
		require.NoError(t, err)

		assert.True(t, log.Changed())

		target, err := os.Readlink(env.Path("/usr/local/app"))
		require.NoError(t, err)
		assert.Equal(t, "/opt/app/current", target)
	})

	t.Run("replaces a file", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.WriteFile("/usr/local/app", "not a link")

		_, err := env.Apply(env.Context(), CreateLink("/usr/local/app", "/opt/app/current"))
		require.NoError(t, err)

		target, err := os.Readlink(env.Path("/usr/local/app"))
		require.NoError(t, err)
		assert.Equal(t, "/opt/app/current", target)
	})

	t.Run("dry run leaves the filesystem alone", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		require.NoError(t, os.MkdirAll(env.Path("/usr/local"), 0o755))
		require.NoError(t, os.Symlink("/opt/app/old", env.Path("/usr/local/app")))

		log, err := env.Apply(env.DryRunContext(), CreateLink("/usr/local/app", "/opt/app/current"))
		require.NoError(t, err)

		assert.True(t, log.Changed())

		target, err := os.Readlink(env.Path("/usr/local/app"))
		require.NoError(t, err)
		assert.Equal(t, "/opt/app/old", target)
	})

	t.Run("dry run does not create the parent", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)

		l := CreateLink("/usr/local/app", "/opt/app/current")
		l.CreateDirIfMissing = true

		log, err := env.Apply(env.DryRunContext(), l)
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.False(t, viaduct.DirExists(env.Path("/usr/local")))
	})

	t.Run("dry run reports a correct link as a noop", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		require.NoError(t, os.MkdirAll(env.Path("/usr/local"), 0o755))
		require.NoError(t, os.Symlink("/opt/app/current", env.Path("/usr/local/app")))

		log, err := env.Apply(env.DryRunContext(), CreateLink("/usr/local/app", "/opt/app/current"))
		require.NoError(t, err)

		assert.False(t, log.Changed())
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		require.NoError(t, os.MkdirAll(env.Path("/usr/local"), 0o755))
		require.NoError(t, os.Symlink("/opt/app/current", env.Path("/usr/local/app")))

		log, err := env.Apply(env.Context(), DeleteLink("/usr/local/app"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.False(t, viaduct.LinkExists(env.Path("/usr/local/app")))

		log, err = env.Apply(env.Context(), DeleteLink("/usr/local/app"))
		require.NoError(t, err)

		assert.False(t, log.Changed())
	})

	t.Run("dry run delete leaves the link", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		require.NoError(t, os.MkdirAll(env.Path("/usr/local"), 0o755))
		require.NoError(t, os.Symlink("/opt/app/current", env.Path("/usr/local/app")))

		log, err := env.Apply(env.DryRunContext(), DeleteLink("/usr/local/app"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.True(t, viaduct.LinkExists(env.Path("/usr/local/app")))
	})
}
