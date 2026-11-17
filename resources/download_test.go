package resources

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surminus/viaduct"
	"github.com/surminus/viaduct/viaducttest"
)

// sha256 of "OK"
const okSum = "565339bc4d33d72817b583024112eb7f5cdf3e5eef0252d6ec1b9c9a94e12bb3"

// serve returns the URL of a server that replies with status and body, and a
// count of the requests it has had.
func serve(t *testing.T, status int, body string) (string, *atomic.Int32) {
	t.Helper()

	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return srv.URL, &hits
}

// dirEntries lists a directory under the root, to look for temporary files.
func dirEntries(t *testing.T, env *viaducttest.Env, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(env.Path(dir))
	require.NoError(t, err)

	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}

	return names
}

func TestDownloadRun(t *testing.T) {
	t.Parallel()

	t.Run("downloads with the default mode", func(t *testing.T) {
		t.Parallel()

		u, _ := serve(t, http.StatusOK, "OK")
		env := viaducttest.NewEnv(t)
		env.WriteFile("/opt/app/.keep", "")

		log, err := env.Apply(env.Context(), Wget(u, "/opt/app/file"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, "OK", env.ReadFile("/opt/app/file"))
		assertMode(t, env.Path("/opt/app/file"), DefaultFilePermissions)
	})

	t.Run("creates a missing parent, then is a noop", func(t *testing.T) {
		t.Parallel()

		u, hits := serve(t, http.StatusOK, "OK")
		env := viaducttest.NewEnv(t)
		d := &Download{URL: u, Path: "/opt/app/nested/file", CreateDirIfMissing: true, NotIfExists: true}

		log, err := env.Apply(env.Context(), d)
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, "OK", env.ReadFile("/opt/app/nested/file"))

		log, err = env.Apply(env.Context(), d)
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Equal(t, int32(1), hits.Load())
	})

	t.Run("a matching checksum downloads", func(t *testing.T) {
		t.Parallel()

		u, _ := serve(t, http.StatusOK, "OK")
		env := viaducttest.NewEnv(t)

		_, err := env.Apply(env.Context(), &Download{URL: u, Path: "/opt/file", CreateDirIfMissing: true, Checksum: okSum})
		require.NoError(t, err)
		assert.Equal(t, "OK", env.ReadFile("/opt/file"))
	})

	t.Run("a mismatched checksum leaves nothing behind", func(t *testing.T) {
		t.Parallel()

		u, _ := serve(t, http.StatusOK, "OK")
		env := viaducttest.NewEnv(t)
		env.WriteFile("/opt/.keep", "")

		_, err := env.Apply(env.Context(), &Download{URL: u, Path: "/opt/file", Checksum: strings.Repeat("0", 64)})
		assert.ErrorContains(t, err, "checksum mismatch")
		assert.Equal(t, []string{".keep"}, dirEntries(t, env, "/opt"))
	})

	t.Run("an uppercase checksum matches", func(t *testing.T) {
		t.Parallel()

		u, hits := serve(t, http.StatusOK, "OK")
		env := viaducttest.NewEnv(t)
		env.WriteFile("/opt/.keep", "")
		d := &Download{URL: u, Path: "/opt/file", Checksum: strings.ToUpper(okSum)}

		_, err := env.Apply(env.Context(), d)
		require.NoError(t, err)
		assert.Equal(t, "OK", env.ReadFile("/opt/file"))

		log, err := env.Apply(env.Context(), d)
		require.NoError(t, err)
		assert.False(t, log.Changed(), "the existing file matches too")
		assert.Equal(t, int32(1), hits.Load())
	})

	t.Run("matching checksum is a noop without a download", func(t *testing.T) {
		t.Parallel()

		u, hits := serve(t, http.StatusOK, "OK")
		env := viaducttest.NewEnv(t)
		env.WriteFile("/opt/app/file", "OK")

		log, err := env.Apply(env.Context(), &Download{URL: u, Path: "/opt/app/file", Checksum: okSum})
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Zero(t, hits.Load())
	})

	t.Run("a symlink at the path is kept and its target replaced", func(t *testing.T) {
		t.Parallel()

		u, _ := serve(t, http.StatusOK, "OK")
		env := viaducttest.NewEnv(t)
		env.WriteFile("/opt/app/real", "old")
		require.NoError(t, os.Symlink("real", env.Path("/opt/app/file")))

		_, err := env.Apply(env.Context(), &Download{URL: u, Path: "/opt/app/file", Permissions: Permissions{Mode: 0o750}})
		require.NoError(t, err)

		info, err := os.Lstat(env.Path("/opt/app/file"))
		require.NoError(t, err)
		assert.NotZero(t, info.Mode()&fs.ModeSymlink, "the link is kept")
		assert.Equal(t, "OK", env.ReadFile("/opt/app/real"))
		assertMode(t, env.Path("/opt/app/real"), 0o750)
	})

	t.Run("an absolute symlink under the root stays under it", func(t *testing.T) {
		t.Parallel()

		u, _ := serve(t, http.StatusOK, "OK")
		env := viaducttest.NewEnv(t)

		// The link names a path on the host, which under Root is Root's
		host := filepath.Join(t.TempDir(), "file")
		env.WriteFile(host, "old")
		require.NoError(t, os.MkdirAll(env.Path("/opt/app"), 0o755))
		require.NoError(t, os.Symlink(host, env.Path("/opt/app/file")))

		_, err := env.Apply(env.Context(), &Download{URL: u, Path: "/opt/app/file"})
		require.NoError(t, err)

		assert.Equal(t, "OK", env.ReadFile(host))
		assert.NoFileExists(t, host)
	})

	t.Run("dry run downloads nothing", func(t *testing.T) {
		t.Parallel()

		u, hits := serve(t, http.StatusOK, "OK")
		env := viaducttest.NewEnv(t)

		log, err := env.Apply(env.DryRunContext(), &Download{URL: u, Path: "/opt/app/file", CreateDirIfMissing: true})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.False(t, viaduct.DirExists(env.Path("/opt/app")))
		assert.Zero(t, hits.Load())
	})

	t.Run("dry run with a mismatched checksum reports the download", func(t *testing.T) {
		t.Parallel()

		u, hits := serve(t, http.StatusOK, "OK")
		env := viaducttest.NewEnv(t)
		env.WriteFile("/opt/app/file", "old")

		log, err := env.Apply(env.DryRunContext(), &Download{URL: u, Path: "/opt/app/file", Checksum: okSum})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, "old", env.ReadFile("/opt/app/file"))
		assert.Zero(t, hits.Load())
	})

	t.Run("dry run with a matching checksum is a noop", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.WriteFile("/opt/app/file", "OK")

		log, err := env.Apply(env.DryRunContext(), &Download{URL: "http://127.0.0.1:1/", Path: "/opt/app/file", Checksum: okSum})
		require.NoError(t, err)

		assert.False(t, log.Changed())
	})
}

func TestDownloadKeepsExistingFileOnFailure(t *testing.T) {
	t.Parallel()

	t.Run("server error", func(t *testing.T) {
		t.Parallel()

		u, _ := serve(t, http.StatusInternalServerError, "")
		env := viaducttest.NewEnv(t)
		env.WriteFile("/opt/app/file", "previous")

		_, err := env.Apply(env.Context(), &Download{URL: u, Path: "/opt/app/file", Checksum: okSum})
		assert.ErrorContains(t, err, "status code 500")

		assert.Equal(t, "previous", env.ReadFile("/opt/app/file"))
		assert.Equal(t, []string{"file"}, dirEntries(t, env, "/opt/app"), "no temporary file is left behind")
	})

	t.Run("checksum mismatch", func(t *testing.T) {
		t.Parallel()

		u, _ := serve(t, http.StatusOK, "tampered")
		env := viaducttest.NewEnv(t)
		env.WriteFile("/opt/app/file", "previous")

		_, err := env.Apply(env.Context(), &Download{URL: u, Path: "/opt/app/file", Checksum: okSum})
		assert.ErrorContains(t, err, "checksum mismatch")

		assert.Equal(t, "previous", env.ReadFile("/opt/app/file"))
		assert.Equal(t, []string{"file"}, dirEntries(t, env, "/opt/app"), "no temporary file is left behind")
	})

	t.Run("replaces the file rather than rewriting it", func(t *testing.T) {
		t.Parallel()

		u, _ := serve(t, http.StatusOK, "OK")
		env := viaducttest.NewEnv(t)
		env.WriteFile("/opt/app/file", "previous")

		before, err := os.Stat(env.Path("/opt/app/file"))
		require.NoError(t, err)

		_, err = env.Apply(env.Context(), &Download{URL: u, Path: "/opt/app/file", Checksum: okSum})
		require.NoError(t, err)

		after, err := os.Stat(env.Path("/opt/app/file"))
		require.NoError(t, err)

		assert.Equal(t, "OK", env.ReadFile("/opt/app/file"))
		assert.False(t, os.SameFile(before, after))
	})

	t.Run("cancelling stops the download", func(t *testing.T) {
		t.Parallel()

		release := make(chan struct{})

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("partial"))
			w.(http.Flusher).Flush()
			<-release
		}))
		// Cleanups run last first: release the handler, then close
		t.Cleanup(srv.Close)
		t.Cleanup(func() { close(release) })

		env := viaducttest.NewEnv(t)
		env.WriteFile("/opt/app/file", "previous")

		ctx, cancel := context.WithTimeout(env.Context(), 50*time.Millisecond)
		defer cancel()

		start := time.Now()
		_, err := env.Apply(ctx, &Download{URL: srv.URL, Path: "/opt/app/file"})
		require.Error(t, err)

		assert.Less(t, time.Since(start), 5*time.Second)
		assert.Equal(t, "previous", env.ReadFile("/opt/app/file"))
		assert.Equal(t, []string{"file"}, dirEntries(t, env, "/opt/app"), "no temporary file is left behind")
	})
}

func TestDownloadRedirects(t *testing.T) {
	t.Parallel()

	req := func(raw string) *http.Request {
		u, err := url.Parse(raw)
		require.NoError(t, err)

		return &http.Request{URL: u}
	}

	t.Run("https to http is refused", func(t *testing.T) {
		err := checkRedirect(req("http://example.com/file"), []*http.Request{req("https://example.com/file")})
		assert.ErrorContains(t, err, "refusing a redirect from https")
	})

	t.Run("a downgrade later in the chain is refused", func(t *testing.T) {
		err := checkRedirect(req("http://example.com/c"), []*http.Request{req("http://example.com/a"), req("https://example.com/b")})
		assert.Error(t, err)
	})

	t.Run("http to https and https to https are allowed", func(t *testing.T) {
		assert.NoError(t, checkRedirect(req("https://example.com/b"), []*http.Request{req("http://example.com/a")}))
		assert.NoError(t, checkRedirect(req("https://cdn.example.com/b"), []*http.Request{req("https://example.com/a")}))
	})

	t.Run("too many redirects", func(t *testing.T) {
		via := make([]*http.Request, maxRedirects)
		for i := range via {
			via[i] = req("http://example.com/")
		}

		assert.ErrorContains(t, checkRedirect(req("http://example.com/"), via), "stopped after")
	})
}
