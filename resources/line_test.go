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
	if err := line.PreflightChecks(testLogger); err != nil {
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

		err := l.PreflightChecks(testLogger)
		assert.EqualError(t, err, "required parameter: Path")
	})

	t.Run("requires line", func(t *testing.T) {
		l := &Line{Path: "/tmp/test"}

		err := l.PreflightChecks(testLogger)
		assert.EqualError(t, err, "required parameter: Line")
	})

	t.Run("delete requires line or match", func(t *testing.T) {
		l := &Line{Path: "/tmp/test", Delete: true}

		err := l.PreflightChecks(testLogger)
		assert.EqualError(t, err, "delete requires one of Line or Match")
	})

	t.Run("invalid match expression", func(t *testing.T) {
		l := &Line{Path: "/tmp/test", Line: "foo", Match: "(["}

		err := l.PreflightChecks(testLogger)
		assert.Error(t, err)
	})
}

func TestLine(t *testing.T) {
	t.Run("appends to existing file", func(t *testing.T) {
		l := newTestLine(t, "first\n", &Line{Line: "second"})

		err := l.Run(testLogger)
		assert.NoError(t, err)
		assert.Equal(t, "first\nsecond\n", readTestFile(t, l.Path))
	})

	t.Run("noop when line present", func(t *testing.T) {
		l := newTestLine(t, "first\nsecond\n", &Line{Line: "second"})

		err := l.Run(testLogger)
		assert.NoError(t, err)
		assert.Equal(t, "first\nsecond\n", readTestFile(t, l.Path))
	})

	t.Run("creates missing file", func(t *testing.T) {
		l := newTestLine(t, "", &Line{Line: "first"})

		err := l.Run(testLogger)
		assert.NoError(t, err)
		assert.Equal(t, "first\n", readTestFile(t, l.Path))
	})

	t.Run("replaces matching line", func(t *testing.T) {
		l := newTestLine(t, "setting=old\nother\n", &Line{Line: "setting=new", Match: "^setting="})

		err := l.Run(testLogger)
		assert.NoError(t, err)
		assert.Equal(t, "setting=new\nother\n", readTestFile(t, l.Path))
	})

	t.Run("removes duplicate matches", func(t *testing.T) {
		l := newTestLine(t, "setting=old\nsetting=older\n", &Line{Line: "setting=new", Match: "^setting="})

		err := l.Run(testLogger)
		assert.NoError(t, err)
		assert.Equal(t, "setting=new\n", readTestFile(t, l.Path))
	})

	t.Run("noop when match already correct", func(t *testing.T) {
		l := newTestLine(t, "setting=new\n", &Line{Line: "setting=new", Match: "^setting="})

		err := l.Run(testLogger)
		assert.NoError(t, err)
		assert.Equal(t, "setting=new\n", readTestFile(t, l.Path))
	})

	t.Run("appends when no match", func(t *testing.T) {
		l := newTestLine(t, "other\n", &Line{Line: "setting=new", Match: "^setting="})

		err := l.Run(testLogger)
		assert.NoError(t, err)
		assert.Equal(t, "other\nsetting=new\n", readTestFile(t, l.Path))
	})

	t.Run("preserves file mode", func(t *testing.T) {
		l := newTestLine(t, "first\n", &Line{Line: "second"})

		err := os.Chmod(l.Path, 0o600)
		if err != nil {
			t.Fatal(err)
		}

		err = l.Run(testLogger)
		assert.NoError(t, err)

		info, err := os.Stat(l.Path)
		assert.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode())
	})

	t.Run("deletes matching lines", func(t *testing.T) {
		l := newTestLine(t, "keep\nremove=1\nremove=2\n", &Line{Match: "^remove=", Delete: true})

		err := l.Run(testLogger)
		assert.NoError(t, err)
		assert.Equal(t, "keep\n", readTestFile(t, l.Path))
	})

	t.Run("deletes exact line", func(t *testing.T) {
		l := newTestLine(t, "keep\nremove\n", &Line{Line: "remove", Delete: true})

		err := l.Run(testLogger)
		assert.NoError(t, err)
		assert.Equal(t, "keep\n", readTestFile(t, l.Path))
	})

	t.Run("delete noop when nothing matches", func(t *testing.T) {
		l := newTestLine(t, "keep\n", &Line{Match: "^remove=", Delete: true})

		err := l.Run(testLogger)
		assert.NoError(t, err)
		assert.Equal(t, "keep\n", readTestFile(t, l.Path))
	})

	t.Run("delete noop when file missing", func(t *testing.T) {
		l := newTestLine(t, "", &Line{Match: "^remove=", Delete: true})

		err := l.Run(testLogger)
		assert.NoError(t, err)
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
			if err := l.PreflightChecks(testLogger); err != nil {
				t.Error(err)
				return
			}

			if err := l.Run(testLogger); err != nil {
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
}
