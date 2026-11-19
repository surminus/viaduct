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

// newCopySource writes a tree to copy: a file, an executable in a
// subdirectory, and a relative symlink
func newCopySource(t *testing.T, env *viaducttest.Env) {
	t.Helper()

	env.WriteFile("/src/config", "a=1\n")
	env.WriteFile("/src/bin/tool", "#!/bin/sh\n")
	require.NoError(t, os.Chmod(env.Path("/src/bin/tool"), 0o755))
	require.NoError(t, os.Symlink("bin/tool", env.Path("/src/tool")))
}

func TestCopyDir(t *testing.T) {
	t.Parallel()

	t.Run("copies the tree, then is a noop", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		newCopySource(t, env)

		c := &CopyDir{Source: "/src", Path: "/dest"}

		log, err := env.Apply(env.Context(), c)
		require.NoError(t, err)
		assert.True(t, log.Changed())

		assert.Equal(t, "a=1\n", env.ReadFile("/dest/config"))
		assert.Equal(t, "#!/bin/sh\n", env.ReadFile("/dest/bin/tool"))

		info, err := os.Stat(env.Path("/dest/bin/tool"))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())

		target, err := os.Readlink(env.Path("/dest/tool"))
		require.NoError(t, err)
		assert.Equal(t, "bin/tool", target)

		log, err = env.Apply(env.Context(), c)
		require.NoError(t, err)
		assert.False(t, log.Changed())
	})

	t.Run("updates what differs and leaves other entries alone", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		newCopySource(t, env)

		c := &CopyDir{Source: "/src", Path: "/dest"}

		_, err := env.Apply(env.Context(), c)
		require.NoError(t, err)

		env.WriteFile("/src/config", "a=2\n")
		require.NoError(t, os.Chmod(env.Path("/src/bin/tool"), 0o700))
		require.NoError(t, os.Remove(env.Path("/src/tool")))
		require.NoError(t, os.Symlink("bin/other", env.Path("/src/tool")))
		env.WriteFile("/dest/extra", "kept\n")

		log, err := env.Apply(env.Context(), c)
		require.NoError(t, err)
		assert.True(t, log.Changed())

		assert.Equal(t, "a=2\n", env.ReadFile("/dest/config"))
		assert.Equal(t, "kept\n", env.ReadFile("/dest/extra"))

		info, err := os.Stat(env.Path("/dest/bin/tool"))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())

		target, err := os.Readlink(env.Path("/dest/tool"))
		require.NoError(t, err)
		assert.Equal(t, "bin/other", target)

		entries, err := os.ReadDir(env.Path("/dest"))
		require.NoError(t, err)
		for _, e := range entries {
			assert.NotContains(t, e.Name(), "viaduct-tmp")
		}
	})

	t.Run("logs one line with counts", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		newCopySource(t, env)
		env.WriteFile("/dest/config", "old\n")

		_, err := env.Apply(env.Context(), &CopyDir{Source: "/src", Path: "/dest"})
		require.NoError(t, err)

		var changed []viaduct.Event
		for _, e := range env.Recorder.Events() {
			if e.Level == viaduct.LevelChanged {
				changed = append(changed, e)
			}
		}

		require.Len(t, changed, 1)
		assert.Equal(t, "copied", changed[0].Message)

		created, _ := changed[0].Field("created")
		updated, _ := changed[0].Field("updated")
		assert.Equal(t, "3", created, "bin, bin/tool and tool")
		assert.Equal(t, "1", updated, "config")
	})

	t.Run("dry run writes nothing", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		newCopySource(t, env)

		log, err := env.Apply(env.DryRunContext(), &CopyDir{Source: "/src", Path: "/dest"})
		require.NoError(t, err)
		assert.True(t, log.Changed())
		assert.NoDirExists(t, env.Path("/dest"))

		env.WriteFile("/dest/config", "old\n")

		log, err = env.Apply(env.DryRunContext(), &CopyDir{Source: "/src", Path: "/dest"})
		require.NoError(t, err)
		assert.True(t, log.Changed())
		assert.Equal(t, "old\n", env.ReadFile("/dest/config"))
		assert.NoFileExists(t, env.Path("/dest/bin/tool"))
	})

	t.Run("dry run sees through what a real run would replace", func(t *testing.T) {
		t.Parallel()

		outside, _ := outsideVictim(t)

		for name, setup := range map[string]func(env *viaducttest.Env){
			"a file":                func(env *viaducttest.Env) { env.WriteFile("/dest/bin", "file\n") },
			"a symlink out of Path": func(env *viaducttest.Env) { require.NoError(t, os.Symlink(outside, env.Path("/dest/bin"))) },
		} {
			t.Run(name, func(t *testing.T) {
				env := viaducttest.NewEnv(t)
				newCopySource(t, env)
				require.NoError(t, os.MkdirAll(env.Path("/dest"), 0o755))
				setup(env)

				log, err := env.Apply(env.DryRunContext(), &CopyDir{Source: "/src", Path: "/dest"})
				require.NoError(t, err)
				assert.True(t, log.Changed())

				_, err = env.Apply(env.Context(), &CopyDir{Source: "/src", Path: "/dest"})
				require.NoError(t, err)
				assert.Equal(t, "#!/bin/sh\n", env.ReadFile("/dest/bin/tool"))
			})
		}
	})

	t.Run("logs one line when Path is created", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		newCopySource(t, env)

		_, err := env.Apply(env.Context(), &CopyDir{Source: "/src", Path: "/new/dest"})
		require.NoError(t, err)

		assert.Equal(t, []string{"copied"}, env.Recorder.Messages(viaduct.LevelChanged))

		info, err := os.Stat(env.Path("/new"))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), "parents of Path are not given the mode of Source")
	})

	t.Run("copies directories that are not writable", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.WriteFile("/src/sub/file", "content\n")
		require.NoError(t, os.Chmod(env.Path("/src/sub"), 0o555))
		require.NoError(t, os.Chmod(env.Path("/src"), 0o555))
		t.Cleanup(func() {
			_ = os.Chmod(env.Path("/src"), 0o755)
			_ = os.Chmod(env.Path("/src/sub"), 0o755)
			_ = os.Chmod(env.Path("/dest"), 0o755)
			_ = os.Chmod(env.Path("/dest/sub"), 0o755)
		})

		c := &CopyDir{Source: "/src", Path: "/dest"}

		_, err := env.Apply(env.Context(), c)
		require.NoError(t, err)
		assert.Equal(t, "content\n", env.ReadFile("/dest/sub/file"))

		for _, dir := range []string{"/dest", "/dest/sub"} {
			info, err := os.Stat(env.Path(dir))
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o555), info.Mode().Perm(), dir)
		}

		log, err := env.Apply(env.Context(), c)
		require.NoError(t, err)
		assert.False(t, log.Changed())
	})

	t.Run("a Source entry named like a temporary file is kept", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.WriteFile("/src/.x.viaduct-tmp", "kept\n")
		env.WriteFile("/src/x", "x\n")

		c := &CopyDir{Source: "/src", Path: "/dest"}

		_, err := env.Apply(env.Context(), c)
		require.NoError(t, err)
		assert.Equal(t, "kept\n", env.ReadFile("/dest/.x.viaduct-tmp"))

		log, err := env.Apply(env.Context(), c)
		require.NoError(t, err)
		assert.False(t, log.Changed())
	})

	t.Run("dry run with a Source an earlier resource creates", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)

		_, err := env.Apply(env.DryRunContext(), &CopyDir{Source: "/src", Path: "/dest"})
		require.NoError(t, err)
	})

	t.Run("a symlink in Path is replaced, not written through", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		newCopySource(t, env)

		outside, victim := outsideVictim(t)
		require.NoError(t, os.MkdirAll(env.Path("/dest"), 0o755))
		require.NoError(t, os.Symlink(victim, env.Path("/dest/config")))
		require.NoError(t, os.Symlink(outside, env.Path("/dest/bin")))

		_, err := env.Apply(env.Context(), &CopyDir{Source: "/src", Path: "/dest"})
		require.NoError(t, err)

		assert.Equal(t, "a=1\n", env.ReadFile("/dest/config"))
		assert.Equal(t, "#!/bin/sh\n", env.ReadFile("/dest/bin/tool"))

		info, err := os.Lstat(env.Path("/dest/bin"))
		require.NoError(t, err)
		assert.True(t, info.IsDir())

		assertUntouched(t, victim)
		assert.NoFileExists(t, filepath.Join(outside, "tool"))
	})

	t.Run("a directory in the way fails", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		newCopySource(t, env)
		require.NoError(t, os.MkdirAll(env.Path("/dest/config"), 0o755))

		_, err := env.Apply(env.Context(), &CopyDir{Source: "/src", Path: "/dest"})
		assert.ErrorContains(t, err, "copying config: a directory is in the way")
	})

	t.Run("preflight", func(t *testing.T) {
		t.Parallel()

		assert.EqualError(t, (&CopyDir{Path: "/a"}).PreflightChecks(testCtx, testLogger), "required parameter: Source")
		assert.EqualError(t, (&CopyDir{Source: "/a"}).PreflightChecks(testCtx, testLogger), "required parameter: Path")
		assert.EqualError(t, (&CopyDir{Source: "/a", Path: "/a/"}).PreflightChecks(testCtx, testLogger), "cannot copy /a to /a, since one contains the other")
		assert.EqualError(t, (&CopyDir{Source: "/a", Path: "/a/b"}).PreflightChecks(testCtx, testLogger), "cannot copy /a to /a/b, since one contains the other")
		assert.EqualError(t, (&CopyDir{Source: "/a/b", Path: "/a"}).PreflightChecks(testCtx, testLogger), "cannot copy /a/b to /a, since one contains the other")
		assert.NoError(t, (&CopyDir{Source: "/a", Path: "/ab"}).PreflightChecks(testCtx, testLogger))
	})
}
