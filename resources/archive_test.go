package resources

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surminus/viaduct"
	"github.com/surminus/viaduct/viaducttest"
)

func newTestTarGz(t *testing.T, files map[string]string) string {
	path := filepath.Join(t.TempDir(), "test.tar.gz")

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	for name, content := range files {
		err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o755,
			Size: int64(len(content)),
		})
		if err != nil {
			t.Fatal(err)
		}

		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	return path
}

// newTestTarGzHeaders builds a tarball from explicit headers, so tests can
// include symlink and hardlink entries. Headers are written in order, and
// content is taken from each header's PAXRecords["content"] if present.
func newTestTarGzHeaders(t *testing.T, headers []*tar.Header, contents map[string]string) string {
	path := filepath.Join(t.TempDir(), "test.tar.gz")

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	for _, hdr := range headers {
		content := contents[hdr.Name]
		hdr.Size = int64(len(content))

		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}

		if content != "" {
			if _, err := tw.Write([]byte(content)); err != nil {
				t.Fatal(err)
			}
		}
	}

	return path
}

func newTestZip(t *testing.T, files map[string]string) string {
	path := filepath.Join(t.TempDir(), "test.zip")

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	return path
}

func TestArchivePreflightChecks(t *testing.T) {
	t.Run("requires path", func(t *testing.T) {
		a := &Archive{Dest: "/tmp"}

		err := a.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "required parameter: Path")
	})

	t.Run("requires dest", func(t *testing.T) {
		a := &Archive{Path: "/tmp/test.tar.gz"}

		err := a.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "required parameter: Dest")
	})

	t.Run("unrecognised format", func(t *testing.T) {
		a := &Archive{Path: "/tmp/test.rar", Dest: "/tmp"}

		err := a.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "unrecognised archive format: /tmp/test.rar")
	})

	t.Run("negative strip", func(t *testing.T) {
		a := &Archive{Path: "/tmp/test.tar.gz", Dest: "/tmp", Strip: -1}

		err := a.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "strip must not be negative")
	})
}

func TestArchive(t *testing.T) {
	t.Run("extract tar.gz with strip and pick", func(t *testing.T) {
		path := newTestTarGz(t, map[string]string{
			"node_exporter-1.7.0/node_exporter": "binary",
			"node_exporter-1.7.0/LICENSE":       "license",
		})
		dest := t.TempDir()

		a := &Archive{
			Path:  path,
			Dest:  dest,
			Strip: 1,
			Pick:  []string{"node_exporter"},
		}

		err := a.Run(testCtx, testLogger)
		assert.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(dest, "node_exporter"))
		assert.NoError(t, err)
		assert.Equal(t, "binary", string(content))

		info, err := os.Stat(filepath.Join(dest, "node_exporter"))
		assert.NoError(t, err)
		assert.Equal(t, os.FileMode(0o755), info.Mode())

		assert.NoFileExists(t, filepath.Join(dest, "LICENSE"))
	})

	t.Run("extract zip", func(t *testing.T) {
		path := newTestZip(t, map[string]string{
			"dir/file": "content",
		})
		dest := t.TempDir()

		a := &Archive{Path: path, Dest: dest}

		err := a.Run(testCtx, testLogger)
		assert.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(dest, "dir", "file"))
		assert.NoError(t, err)
		assert.Equal(t, "content", string(content))
	})

	t.Run("path traversal entry fails", func(t *testing.T) {
		path := newTestTarGz(t, map[string]string{
			"../evil": "evil",
		})
		dest := t.TempDir()

		a := &Archive{Path: path, Dest: dest}

		err := a.Run(testCtx, testLogger)
		assert.EqualError(t, err, "archive entry ../evil is outside Dest")

		assert.NoFileExists(t, filepath.Join(filepath.Dir(dest), "evil"))
	})

	t.Run("path traversal entry in a zip fails", func(t *testing.T) {
		path := newTestZip(t, map[string]string{
			"../evil": "evil",
		})
		dest := t.TempDir()

		a := &Archive{Path: path, Dest: dest}

		err := a.Run(testCtx, testLogger)
		assert.EqualError(t, err, "archive entry ../evil is outside Dest")

		assert.NoFileExists(t, filepath.Join(filepath.Dir(dest), "evil"))
	})

	t.Run("not if exists", func(t *testing.T) {
		path := newTestTarGz(t, map[string]string{
			"file": "new",
		})
		dest := t.TempDir()

		err := os.WriteFile(filepath.Join(dest, "file"), []byte("old"), 0o644)
		if err != nil {
			t.Fatal(err)
		}

		a := &Archive{
			Path:        path,
			Dest:        dest,
			Pick:        []string{"file"},
			NotIfExists: true,
		}

		err = a.Run(testCtx, testLogger)
		assert.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(dest, "file"))
		assert.NoError(t, err)
		assert.Equal(t, "old", string(content))
	})

	t.Run("unsafe symlink fails and cannot be written through", func(t *testing.T) {
		// A symlink escaping dest, followed by a file written through it,
		// is the classic tar-slip
		path := newTestTarGzHeaders(t, []*tar.Header{
			{Name: "escape", Typeflag: tar.TypeSymlink, Linkname: "../../outside", Mode: 0o777},
			{Name: "escape/pwned", Typeflag: tar.TypeReg, Mode: 0o644},
		}, map[string]string{"escape/pwned": "owned"})

		base := t.TempDir()
		dest := filepath.Join(base, "dest")

		a := &Archive{Path: path, Dest: dest}

		err := a.Run(testCtx, testLogger)
		assert.EqualError(t, err, "symlink escape points outside Dest: ../../outside")

		assert.NoFileExists(t, filepath.Join(dest, "escape"))
		assert.NoFileExists(t, filepath.Join(base, "outside"))
		assert.NoFileExists(t, filepath.Join(filepath.Dir(base), "outside"))
	})

	t.Run("absolute symlink target fails", func(t *testing.T) {
		path := newTestTarGzHeaders(t, []*tar.Header{
			{Name: "abs", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0o777},
		}, nil)

		dest := t.TempDir()

		a := &Archive{Path: path, Dest: dest}

		err := a.Run(testCtx, testLogger)
		assert.EqualError(t, err, "symlink abs points outside Dest: /etc/passwd")
		assert.NoFileExists(t, filepath.Join(dest, "abs"))
	})

	t.Run("symlink within dest is allowed", func(t *testing.T) {
		path := newTestTarGzHeaders(t, []*tar.Header{
			{Name: "real", Typeflag: tar.TypeReg, Mode: 0o644},
			{Name: "alias", Typeflag: tar.TypeSymlink, Linkname: "real", Mode: 0o777},
		}, map[string]string{"real": "content"})

		dest := t.TempDir()

		a := &Archive{Path: path, Dest: dest}

		err := a.Run(testCtx, testLogger)
		assert.NoError(t, err)

		linkTarget, err := os.Readlink(filepath.Join(dest, "alias"))
		assert.NoError(t, err)
		assert.Equal(t, "real", linkTarget)
	})

	t.Run("hardlink is extracted", func(t *testing.T) {
		path := newTestTarGzHeaders(t, []*tar.Header{
			{Name: "original", Typeflag: tar.TypeReg, Mode: 0o644},
			{Name: "duplicate", Typeflag: tar.TypeLink, Linkname: "original", Mode: 0o644},
		}, map[string]string{"original": "shared"})

		dest := t.TempDir()

		a := &Archive{Path: path, Dest: dest}

		err := a.Run(testCtx, testLogger)
		assert.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(dest, "duplicate"))
		assert.NoError(t, err)
		assert.Equal(t, "shared", string(content))
	})

	t.Run("hardlink outside dest fails", func(t *testing.T) {
		path := newTestTarGzHeaders(t, []*tar.Header{
			{Name: "duplicate", Typeflag: tar.TypeLink, Linkname: "../original", Mode: 0o644},
		}, nil)

		a := &Archive{Path: path, Dest: t.TempDir()}

		err := a.Run(testCtx, testLogger)
		assert.EqualError(t, err, "hardlink duplicate points outside Dest: ../original")
	})

	t.Run("hardlink to an entry not extracted fails", func(t *testing.T) {
		path := newTestTarGzHeaders(t, []*tar.Header{
			{Name: "original", Typeflag: tar.TypeReg, Mode: 0o644},
			{Name: "duplicate", Typeflag: tar.TypeLink, Linkname: "original", Mode: 0o644},
		}, map[string]string{"original": "shared"})

		a := &Archive{Path: path, Dest: t.TempDir(), Pick: []string{"duplicate"}}

		err := a.Run(testCtx, testLogger)
		assert.EqualError(t, err, "hardlink duplicate points to original, which was not extracted")
	})
}

// rootedTarGz builds a tarball and puts it at path under the test root
func rootedTarGz(t *testing.T, env *viaducttest.Env, path string, files map[string]string) {
	t.Helper()

	content, err := os.ReadFile(newTestTarGz(t, files))
	require.NoError(t, err)

	env.WriteFile(path, string(content))
}

func TestArchiveRun(t *testing.T) {
	t.Parallel()

	t.Run("extract under the root, then a noop", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		rootedTarGz(t, env, "/tmp/app.tar.gz", map[string]string{"app/bin/app": "binary"})

		a := &Archive{Path: "/tmp/app.tar.gz", Dest: "/opt", Pick: []string{"app/bin/app"}, NotIfExists: true}

		log, err := env.Apply(env.Context(), a)
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, "binary", env.ReadFile("/opt/app/bin/app"))

		log, err = env.Apply(env.Context(), a)
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Empty(t, env.Runner.Commands())
	})

	t.Run("dry run extracts nothing", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		rootedTarGz(t, env, "/tmp/app.tar.gz", map[string]string{"app/bin/app": "binary"})

		log, err := env.Apply(env.DryRunContext(), Extract("/tmp/app.tar.gz", "/opt"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.False(t, viaduct.DirExists(env.Path("/opt")))
	})

	t.Run("dry run reports extracted as a noop", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		env.WriteFile("/opt/app/bin/app", "binary")

		a := &Archive{Path: "/tmp/app.tar.gz", Dest: "/opt", Pick: []string{"app/bin/app"}, NotIfExists: true}

		log, err := env.Apply(env.DryRunContext(), a)
		require.NoError(t, err)

		assert.False(t, log.Changed())
	})

	t.Run("dry run with an archive an earlier resource would create", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)

		log, err := env.Apply(env.DryRunContext(), Extract("/tmp/app.tar.gz", "/opt"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
	})
}

func TestArchiveMarker(t *testing.T) {
	t.Parallel()

	marker, err := archiveMarker(viaduct.Env{Root: "/"}, "/tmp/app.tar.gz", "/opt")
	require.NoError(t, err)

	t.Run("extract, then a noop", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		rootedTarGz(t, env, "/tmp/app.tar.gz", map[string]string{"app/bin/app": "binary"})

		a := Extract("/tmp/app.tar.gz", "/opt")

		log, err := env.Apply(env.Context(), a)
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, "binary", env.ReadFile("/opt/app/bin/app"))

		// The marker is written under the root, not on the host, and
		// outside Dest
		assert.Contains(t, env.ReadFile(marker), "sha256 ")
		assert.NoFileExists(t, marker)
		entries, err := os.ReadDir(env.Path("/opt"))
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, "app", entries[0].Name())

		log, err = env.Apply(env.Context(), a)
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Equal(t, []string{"up-to-date"}, env.Recorder.Messages(viaduct.LevelNoop))
	})

	t.Run("a changed archive extracts again", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		rootedTarGz(t, env, "/tmp/app.tar.gz", map[string]string{"app/bin/app": "v1"})

		a := Extract("/tmp/app.tar.gz", "/opt")

		_, err := env.Apply(env.Context(), a)
		require.NoError(t, err)

		rootedTarGz(t, env, "/tmp/app.tar.gz", map[string]string{"app/bin/app": "v2"})

		log, err := env.Apply(env.Context(), a)
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, "v2", env.ReadFile("/opt/app/bin/app"))
	})

	t.Run("changing what is picked extracts again", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		rootedTarGz(t, env, "/tmp/app.tar.gz", map[string]string{"app/bin/app": "binary", "app/LICENSE": "license"})

		_, err := env.Apply(env.Context(), &Archive{Path: "/tmp/app.tar.gz", Dest: "/opt", Pick: []string{"app/bin/app"}})
		require.NoError(t, err)

		log, err := env.Apply(env.Context(), &Archive{Path: "/tmp/app.tar.gz", Dest: "/opt", Pick: []string{"app/bin/app", "app/LICENSE"}})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, "license", env.ReadFile("/opt/app/LICENSE"))
	})

	t.Run("a removed pick entry extracts again", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		rootedTarGz(t, env, "/tmp/app.tar.gz", map[string]string{"app/bin/app": "binary"})

		a := &Archive{Path: "/tmp/app.tar.gz", Dest: "/opt", Pick: []string{"app/bin/app"}}

		_, err := env.Apply(env.Context(), a)
		require.NoError(t, err)

		require.NoError(t, os.Remove(env.Path("/opt/app/bin/app")))

		log, err := env.Apply(env.Context(), a)
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, "binary", env.ReadFile("/opt/app/bin/app"))
	})

	t.Run("a failed extraction removes the marker", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		rootedTarGz(t, env, "/tmp/app.tar.gz", map[string]string{"app/bin/app": "v1"})

		a := Extract("/tmp/app.tar.gz", "/opt")

		_, err := env.Apply(env.Context(), a)
		require.NoError(t, err)
		require.FileExists(t, env.Path(marker))

		env.WriteFile("/tmp/app.tar.gz", "not an archive")

		_, err = env.Apply(env.Context(), a)
		require.Error(t, err)

		assert.NoFileExists(t, env.Path(marker))
	})

	t.Run("dry run writes nothing", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		rootedTarGz(t, env, "/tmp/app.tar.gz", map[string]string{"app/bin/app": "binary"})

		log, err := env.Apply(env.DryRunContext(), Extract("/tmp/app.tar.gz", "/opt"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.NoFileExists(t, env.Path(marker))
		assert.False(t, viaduct.DirExists(env.Path("/opt")))
	})

	t.Run("dry run reports an extracted archive as a noop", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		rootedTarGz(t, env, "/tmp/app.tar.gz", map[string]string{"app/bin/app": "binary"})

		a := Extract("/tmp/app.tar.gz", "/opt")

		_, err := env.Apply(env.Context(), a)
		require.NoError(t, err)

		log, err := env.Apply(env.DryRunContext(), a)
		require.NoError(t, err)

		assert.False(t, log.Changed())
	})

	t.Run("a missing archive fails outside a dry run", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)

		_, err := env.Apply(env.Context(), Extract("/tmp/app.tar.gz", "/opt"))
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("a removed destination extracts again", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		rootedTarGz(t, env, "/tmp/app.tar.gz", map[string]string{"app/bin/app": "binary"})

		a := Extract("/tmp/app.tar.gz", "/opt")

		_, err := env.Apply(env.Context(), a)
		require.NoError(t, err)

		require.NoError(t, os.RemoveAll(env.Path("/opt")))

		log, err := env.Apply(env.Context(), a)
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, "binary", env.ReadFile("/opt/app/bin/app"))
	})

	t.Run("each archive and destination has its own marker", func(t *testing.T) {
		t.Parallel()

		env := viaduct.Env{Root: "/"}
		a, err := archiveMarker(env, "/tmp/a.tar.gz", "/opt")
		require.NoError(t, err)
		b, err := archiveMarker(env, "/tmp/b.tar.gz", "/opt")
		require.NoError(t, err)
		c, err := archiveMarker(env, "/tmp/a.tar.gz", "/srv")
		require.NoError(t, err)

		assert.NotEqual(t, a, b)
		assert.NotEqual(t, a, c)
		assert.Regexp(t, `^/var/lib/viaduct/archive/[0-9a-f]{32}$`, a)
	})
}

func TestArchiveStateDir(t *testing.T) {
	if viaduct.IsRoot() {
		t.Skip("running as root")
	}

	t.Setenv("XDG_STATE_HOME", "/state")

	marker, err := archiveMarker(viaduct.Env{}, "/tmp/a.tar.gz", "/opt")
	require.NoError(t, err)

	assert.Regexp(t, `^/state/viaduct/archive/[0-9a-f]{32}$`, marker)
}

func TestArchiveRename(t *testing.T) {
	t.Parallel()

	t.Run("extracts an entry under another name", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		rootedTarGz(t, env, "/tmp/agent.tar.gz", map[string]string{"agent-linux": "binary", "README": "readme"})

		a := &Archive{
			Path:   "/tmp/agent.tar.gz",
			Dest:   "/usr/local/bin",
			Pick:   []string{"agent-linux"},
			Rename: map[string]string{"agent-linux": "agent"},
		}

		log, err := env.Apply(env.Context(), a)
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, "binary", env.ReadFile("/usr/local/bin/agent"))
		assert.NoFileExists(t, env.Path("/usr/local/bin/agent-linux"))
		assert.NoFileExists(t, env.Path("/usr/local/bin/README"))

		log, err = env.Apply(env.Context(), a)
		require.NoError(t, err)
		assert.False(t, log.Changed())

		require.NoError(t, os.Remove(env.Path("/usr/local/bin/agent")))

		log, err = env.Apply(env.Context(), a)
		require.NoError(t, err)
		assert.True(t, log.Changed(), "a removed renamed entry extracts again")
	})

	t.Run("changing a rename extracts again", func(t *testing.T) {
		t.Parallel()

		env := viaducttest.NewEnv(t)
		rootedTarGz(t, env, "/tmp/agent.tar.gz", map[string]string{"agent-linux": "binary"})

		_, err := env.Apply(env.Context(), &Archive{Path: "/tmp/agent.tar.gz", Dest: "/opt", Rename: map[string]string{"agent-linux": "a"}})
		require.NoError(t, err)

		log, err := env.Apply(env.Context(), &Archive{Path: "/tmp/agent.tar.gz", Dest: "/opt", Rename: map[string]string{"agent-linux": "b"}})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, "binary", env.ReadFile("/opt/b"))
	})

	t.Run("a rename must stay within Dest", func(t *testing.T) {
		t.Parallel()

		for _, to := range []string{"../agent", "/usr/bin/agent", ""} {
			a := &Archive{Path: "/tmp/agent.tar.gz", Dest: "/opt", Rename: map[string]string{"agent-linux": to}}
			assert.ErrorContains(t, a.PreflightChecks(testCtx, testLogger), "must be a path within Dest", to)
		}
	})
}

func TestArchiveAllowAbsoluteLinks(t *testing.T) {
	t.Parallel()

	t.Run("extracts an absolute symlink", func(t *testing.T) {
		t.Parallel()

		path := newTestTarGzHeaders(t, []*tar.Header{
			{Name: "certs", Typeflag: tar.TypeSymlink, Linkname: "/etc/ssl/certs", Mode: 0o777},
		}, nil)
		dest := t.TempDir()

		a := &Archive{Path: path, Dest: dest, AllowAbsoluteLinks: true}
		require.NoError(t, a.Run(testCtx, testLogger))

		link, err := os.Readlink(filepath.Join(dest, "certs"))
		require.NoError(t, err)
		assert.Equal(t, "/etc/ssl/certs", link)
	})

	t.Run("relative symlink outside dest still fails", func(t *testing.T) {
		t.Parallel()

		path := newTestTarGzHeaders(t, []*tar.Header{
			{Name: "escape", Typeflag: tar.TypeSymlink, Linkname: "../outside", Mode: 0o777},
		}, nil)

		a := &Archive{Path: path, Dest: t.TempDir(), AllowAbsoluteLinks: true}
		assert.EqualError(t, a.Run(testCtx, testLogger), "symlink escape points outside Dest: ../outside")
	})

	t.Run("an entry written through an absolute symlink fails", func(t *testing.T) {
		t.Parallel()

		outside := t.TempDir()
		path := newTestTarGzHeaders(t, []*tar.Header{
			{Name: "certs", Typeflag: tar.TypeSymlink, Linkname: outside, Mode: 0o777},
			{Name: "certs/pwned", Typeflag: tar.TypeReg, Mode: 0o644},
		}, map[string]string{"certs/pwned": "owned"})
		dest := t.TempDir()

		a := &Archive{Path: path, Dest: dest, AllowAbsoluteLinks: true}
		err := a.Run(testCtx, testLogger)

		assert.EqualError(t, err, filepath.Join(dest, "certs", "pwned")+" would be written through a symlink to outside Dest")
		assert.NoFileExists(t, filepath.Join(outside, "pwned"))
	})

	t.Run("an entry written through a chain of symlinks fails", func(t *testing.T) {
		t.Parallel()

		outside := t.TempDir()
		path := newTestTarGzHeaders(t, []*tar.Header{
			{Name: "far", Typeflag: tar.TypeSymlink, Linkname: outside, Mode: 0o777},
			{Name: "near", Typeflag: tar.TypeSymlink, Linkname: "far", Mode: 0o777},
			{Name: "near/sub/pwned", Typeflag: tar.TypeReg, Mode: 0o644},
		}, map[string]string{"near/sub/pwned": "owned"})
		dest := t.TempDir()

		a := &Archive{Path: path, Dest: dest, AllowAbsoluteLinks: true}
		err := a.Run(testCtx, testLogger)

		assert.ErrorContains(t, err, "would be written through a symlink to outside Dest")
		assert.NoDirExists(t, filepath.Join(outside, "sub"))
	})

	t.Run("a file replaces a symlink rather than writing through it", func(t *testing.T) {
		t.Parallel()

		outside := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(outside, []byte("original"), 0o644))

		path := newTestTarGzHeaders(t, []*tar.Header{
			// Named differently so the helper gives it no content
			{Name: "./file", Typeflag: tar.TypeSymlink, Linkname: outside, Mode: 0o777},
			{Name: "file", Typeflag: tar.TypeReg, Mode: 0o644},
		}, map[string]string{"file": "replaced"})
		dest := t.TempDir()

		a := &Archive{Path: path, Dest: dest, AllowAbsoluteLinks: true}
		require.NoError(t, a.Run(testCtx, testLogger))

		content, err := os.ReadFile(outside)
		require.NoError(t, err)
		assert.Equal(t, "original", string(content))

		content, err = os.ReadFile(filepath.Join(dest, "file"))
		require.NoError(t, err)
		assert.Equal(t, "replaced", string(content))
	})
}
