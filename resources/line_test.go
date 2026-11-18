package resources

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surminus/viaduct"
	"github.com/surminus/viaduct/viaducttest"
)

func newTestLine(t *testing.T, content string, line *Line) *Line {
	path := filepath.Join(t.TempDir(), "test")

	if content != "" {
		err := os.WriteFile(path, []byte(content), 0o644)
		if err != nil {
			t.Fatal(err)
		}
	}

	line.Path = path
	if err := line.PreflightChecks(testCtx, testLogger); err != nil {
		t.Fatal(err)
	}

	return line
}

func readTestFile(t *testing.T, path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(content)
}

func TestLinePreflightChecks(t *testing.T) {
	t.Run("requires path", func(t *testing.T) {
		l := &Line{Line: "foo"}

		err := l.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "required parameter: Path")
	})

	t.Run("requires line", func(t *testing.T) {
		l := &Line{Path: "/tmp/test"}

		err := l.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "required parameter: Line")
	})

	t.Run("delete requires line or match", func(t *testing.T) {
		l := &Line{Path: "/tmp/test", Delete: true}

		err := l.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "delete requires one of Line or Match")
	})

	t.Run("substitute requires match", func(t *testing.T) {
		l := &Line{Path: "/tmp/test", Substitute: true}

		err := l.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "substitute requires Match")
	})

	t.Run("substitute cannot delete", func(t *testing.T) {
		l := &Line{Path: "/tmp/test", Match: "x", Substitute: true, Delete: true}

		err := l.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "cannot set both Delete and Substitute")
	})

	t.Run("invalid match expression", func(t *testing.T) {
		l := &Line{Path: "/tmp/test", Line: "foo", Match: "(["}

		err := l.PreflightChecks(testCtx, testLogger)
		assert.Error(t, err)
	})
}

func TestLine(t *testing.T) {
	t.Run("appends to existing file", func(t *testing.T) {
		l := newTestLine(t, "first\n", &Line{Line: "second"})

		err := l.Run(testCtx, testLogger)
		assert.NoError(t, err)
		assert.Equal(t, "first\nsecond\n", readTestFile(t, l.Path))
	})

	t.Run("noop when line present", func(t *testing.T) {
		l := newTestLine(t, "first\nsecond\n", &Line{Line: "second"})

		err := l.Run(testCtx, testLogger)
		assert.NoError(t, err)
		assert.Equal(t, "first\nsecond\n", readTestFile(t, l.Path))
	})

	t.Run("creates missing file", func(t *testing.T) {
		l := newTestLine(t, "", &Line{Line: "first"})

		err := l.Run(testCtx, testLogger)
		assert.NoError(t, err)
		assert.Equal(t, "first\n", readTestFile(t, l.Path))
	})

	t.Run("replaces matching line", func(t *testing.T) {
		l := newTestLine(t, "setting=old\nother\n", &Line{Line: "setting=new", Match: "^setting="})

		err := l.Run(testCtx, testLogger)
		assert.NoError(t, err)
		assert.Equal(t, "setting=new\nother\n", readTestFile(t, l.Path))
	})

	t.Run("removes duplicate matches", func(t *testing.T) {
		l := newTestLine(t, "setting=old\nsetting=older\n", &Line{Line: "setting=new", Match: "^setting="})

		err := l.Run(testCtx, testLogger)
		assert.NoError(t, err)
		assert.Equal(t, "setting=new\n", readTestFile(t, l.Path))
	})

	t.Run("noop when match already correct", func(t *testing.T) {
		l := newTestLine(t, "setting=new\n", &Line{Line: "setting=new", Match: "^setting="})

		err := l.Run(testCtx, testLogger)
		assert.NoError(t, err)
		assert.Equal(t, "setting=new\n", readTestFile(t, l.Path))
	})

	t.Run("appends when no match", func(t *testing.T) {
		l := newTestLine(t, "other\n", &Line{Line: "setting=new", Match: "^setting="})

		err := l.Run(testCtx, testLogger)
		assert.NoError(t, err)
		assert.Equal(t, "other\nsetting=new\n", readTestFile(t, l.Path))
	})

	t.Run("preserves file mode", func(t *testing.T) {
		l := newTestLine(t, "first\n", &Line{Line: "second"})

		err := os.Chmod(l.Path, 0o600)
		if err != nil {
			t.Fatal(err)
		}

		err = l.Run(testCtx, testLogger)
		assert.NoError(t, err)

		info, err := os.Stat(l.Path)
		assert.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode())
	})

	t.Run("deletes matching lines", func(t *testing.T) {
		l := newTestLine(t, "keep\nremove=1\nremove=2\n", &Line{Match: "^remove=", Delete: true})

		err := l.Run(testCtx, testLogger)
		assert.NoError(t, err)
		assert.Equal(t, "keep\n", readTestFile(t, l.Path))
	})

	t.Run("deletes exact line", func(t *testing.T) {
		l := newTestLine(t, "keep\nremove\n", &Line{Line: "remove", Delete: true})

		err := l.Run(testCtx, testLogger)
		assert.NoError(t, err)
		assert.Equal(t, "keep\n", readTestFile(t, l.Path))
	})

	t.Run("delete noop when nothing matches", func(t *testing.T) {
		l := newTestLine(t, "keep\n", &Line{Match: "^remove=", Delete: true})

		err := l.Run(testCtx, testLogger)
		assert.NoError(t, err)
		assert.Equal(t, "keep\n", readTestFile(t, l.Path))
	})

	t.Run("delete noop when file missing", func(t *testing.T) {
		l := newTestLine(t, "", &Line{Match: "^remove=", Delete: true})

		err := l.Run(testCtx, testLogger)
		assert.NoError(t, err)
		assert.NoFileExists(t, l.Path)
	})

	t.Run("substitute removes text within matching lines", func(t *testing.T) {
		content := "auth required pam_unix.so nullok try_first_pass\nauth optional pam_cap.so\n"
		l := newTestLine(t, content, &Line{Match: ` nullok\b`, Substitute: true})

		log := viaduct.NewLogger(nil, "Line", "Update")
		require.NoError(t, l.Run(testCtx, log))
		assert.True(t, log.Changed())
		assert.Equal(t, "auth required pam_unix.so try_first_pass\nauth optional pam_cap.so\n", readTestFile(t, l.Path))

		log = viaduct.NewLogger(nil, "Line", "Update")
		require.NoError(t, l.Run(testCtx, log))
		assert.False(t, log.Changed())
	})

	t.Run("substitute expands groups in every match", func(t *testing.T) {
		l := newTestLine(t, "a=1 b=2\n", &Line{Match: `(\w)=(\d)`, Line: "${1}:$2", Substitute: true})

		require.NoError(t, l.Run(testCtx, testLogger))
		assert.Equal(t, "a:1 b:2\n", readTestFile(t, l.Path))
	})

	t.Run("substitute leaves a missing file alone", func(t *testing.T) {
		l := newTestLine(t, "", &Line{Match: "x", Line: "y", Substitute: true})

		require.NoError(t, l.Run(testCtx, testLogger))
		assert.NoFileExists(t, l.Path)
	})
}

func TestLineConcurrentSameFile(t *testing.T) {
	// Many Line resources appending to the same file concurrently must
	// not lose any line to a read-modify-write race.
	path := filepath.Join(t.TempDir(), "shared")

	const n = 20

	var wg sync.WaitGroup
	wg.Add(n)

	for i := range n {
		go func(i int) {
			defer wg.Done()

			l := &Line{Path: path, Line: fmt.Sprintf("line-%02d", i)}
			if err := l.PreflightChecks(testCtx, testLogger); err != nil {
				t.Error(err)
				return
			}

			if err := l.Run(testCtx, testLogger); err != nil {
				t.Error(err)
			}
		}(i)
	}

	wg.Wait()

	content, err := os.ReadFile(path)
	assert.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	sort.Strings(lines)
	assert.Len(t, lines, n)

	for i := range n {
		assert.Equal(t, fmt.Sprintf("line-%02d", i), lines[i])
	}
}

func TestLineHelpers(t *testing.T) {
	assert.Equal(t, &Line{Path: "/tmp/f", Line: "foo"}, AppendLine("/tmp/f", "foo"))
	assert.Equal(t, &Line{Path: "/tmp/f", Match: "^foo", Line: "foo=1"}, ReplaceLine("/tmp/f", "^foo", "foo=1"))
	assert.Equal(t, &Line{Path: "/tmp/f", Match: "^foo", Delete: true}, DeleteLine("/tmp/f", "^foo"))
	assert.Equal(t, &Line{Path: "/tmp/f", Match: "o+", Line: "0", Substitute: true}, Substitute("/tmp/f", "o+", "0"))
}

func TestLineAtomicWrite(t *testing.T) {
	env := viaducttest.NewEnv(t)
	env.WriteFile("/etc/fstab", "LABEL=root / ext4 defaults 0 1\n")
	require.NoError(t, os.Chmod(env.Path("/etc/fstab"), 0o600))

	before, err := os.Stat(env.Path("/etc/fstab"))
	require.NoError(t, err)

	_, err = env.Apply(env.Context(), AppendLine("/etc/fstab", "tmpfs /tmp tmpfs defaults 0 0"))
	require.NoError(t, err)

	after, err := os.Stat(env.Path("/etc/fstab"))
	require.NoError(t, err)

	assert.Equal(t, "LABEL=root / ext4 defaults 0 1\ntmpfs /tmp tmpfs defaults 0 0\n", env.ReadFile("/etc/fstab"))
	assert.False(t, os.SameFile(before, after), "the file is replaced rather than truncated")
	assert.Equal(t, os.FileMode(0o600), after.Mode().Perm(), "the mode is kept")

	entries, err := os.ReadDir(env.Path("/etc"))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file is left behind")
}
