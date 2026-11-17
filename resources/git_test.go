package resources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surminus/viaduct"
	"github.com/surminus/viaduct/viaducttest"
)

// upstream is a local repository to clone from.
type upstream struct {
	t    *testing.T
	path string
	repo *git.Repository
}

// newUpstream creates a repository on main with one commit. Cloning a local
// path runs git-upload-pack, so it needs git installed.
func newUpstream(t *testing.T) *upstream {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("cloning a local repository needs git")
	}

	path := t.TempDir()

	repo, err := git.PlainInitWithOptions(path, &git.PlainInitOptions{
		InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	})
	require.NoError(t, err)

	u := &upstream{t: t, path: path, repo: repo}
	u.commit("README", "first\n")

	return u
}

// commit writes a file and commits it.
func (u *upstream) commit(name, content string) {
	u.t.Helper()

	require.NoError(u.t, os.WriteFile(filepath.Join(u.path, name), []byte(content), 0o644))

	w, err := u.repo.Worktree()
	require.NoError(u.t, err)

	_, err = w.Add(name)
	require.NoError(u.t, err)

	_, err = w.Commit("add "+name, &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()},
	})
	require.NoError(u.t, err)
}

func TestGit(t *testing.T) {
	t.Run("clones, then deletes", func(t *testing.T) {
		up := newUpstream(t)
		env := viaducttest.NewEnv(t)

		log, err := env.Apply(env.Context(), &Git{Path: "/srv/repo", URL: up.path})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, "first\n", env.ReadFile("/srv/repo/README"))
		assertMode(t, env.Path("/srv/repo"), DefaultDirectoryPermissions)

		log, err = env.Apply(env.Context(), &Git{Path: "/srv/repo", URL: up.path, Delete: true})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.False(t, viaduct.DirExists(env.Path("/srv/repo")))
	})

	t.Run("Ensure pulls a new upstream commit, then is a noop", func(t *testing.T) {
		up := newUpstream(t)
		env := viaducttest.NewEnv(t)

		_, err := env.Apply(env.Context(), Repo("/srv/repo", up.path))
		require.NoError(t, err)

		up.commit("NEW", "second\n")

		log, err := env.Apply(env.Context(), Repo("/srv/repo", up.path))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Contains(t, env.Recorder.Messages(viaduct.LevelChanged), "pulled")
		assert.Equal(t, "second\n", env.ReadFile("/srv/repo/NEW"))

		log, err = env.Apply(env.Context(), Repo("/srv/repo", up.path))
		require.NoError(t, err)
		assert.False(t, log.Changed())
	})

	t.Run("cancelling stops a clone", func(t *testing.T) {
		release := make(chan struct{})

		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			<-release
		}))
		// Cleanups run last first: release the handler, then close
		t.Cleanup(srv.Close)
		t.Cleanup(func() { close(release) })

		env := viaducttest.NewEnv(t)
		env.WriteFile("/srv/.keep", "")

		ctx, cancel := context.WithTimeout(env.Context(), 50*time.Millisecond)
		defer cancel()

		start := time.Now()
		_, err := env.Apply(ctx, &Git{Path: "/srv/repo", URL: srv.URL + "/repo.git"})
		require.Error(t, err)

		assert.Less(t, time.Since(start), 5*time.Second)
		assert.Equal(t, []string{".keep"}, dirEntries(t, env, "/srv"), "nothing is left behind")
	})
}

func TestGitDryRun(t *testing.T) {
	t.Parallel()

	t.Run("reports a clone without cloning", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)

		log, err := env.Apply(env.DryRunContext(), Repo("/srv/repo", "https://example.com/repo.git"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"cloned"}, env.Recorder.Messages(viaduct.LevelChanged))
		assert.False(t, viaduct.DirExists(env.Path("/srv/repo")))
		assert.Empty(t, env.Runner.Commands())
	})

	t.Run("reports a pull without pulling", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		require.NoError(t, os.MkdirAll(env.Path("/srv/repo"), 0o755))

		log, err := env.Apply(env.DryRunContext(), Repo("/srv/repo", "https://example.com/repo.git"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"pulled"}, env.Recorder.Messages(viaduct.LevelChanged))
	})

	t.Run("without Ensure an existing clone is a noop", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		require.NoError(t, os.MkdirAll(env.Path("/srv/repo"), 0o755))

		log, err := env.Apply(env.DryRunContext(), &Git{Path: "/srv/repo", URL: "https://example.com/repo.git"})
		require.NoError(t, err)

		assert.False(t, log.Changed())
	})

	t.Run("delete leaves the repository", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.WriteFile("/srv/repo/.git/HEAD", "ref: refs/heads/main\n")

		log, err := env.Apply(env.DryRunContext(), &Git{Path: "/srv/repo", URL: "https://example.com/repo.git", Delete: true})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.True(t, viaduct.DirExists(env.Path("/srv/repo/.git")))
	})
}
