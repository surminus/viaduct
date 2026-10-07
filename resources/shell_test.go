package resources

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/surminus/viaduct"
)

func runShell(t *testing.T, s *Shell) error {
	t.Helper()

	if err := s.PreflightChecks(testLogger); err != nil {
		t.Fatal(err)
	}

	return s.Run(testLogger)
}

func TestShell(t *testing.T) {
	t.Run("runs a multi-line script", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "out")

		s := Bash(`
			for i in 1 2 3; do
				echo "$i" >> "` + path + `"
			done
		`)

		assert.NoError(t, runShell(t, s))
		content, err := os.ReadFile(path)
		assert.NoError(t, err)
		assert.Equal(t, "1\n2\n3\n", string(content))
	})

	t.Run("runs an indented heredoc", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "out")

		s := Sh(`
			cat > "` + path + `" <<EOF
			hello
			EOF
		`)

		assert.NoError(t, runShell(t, s))
		content, err := os.ReadFile(path)
		assert.NoError(t, err)
		assert.Equal(t, "hello\n", string(content))
	})

	t.Run("stops at the first failing command", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "out")

		s := Bash("false\ntouch " + path)

		assert.Error(t, runShell(t, s))
		assert.NoFileExists(t, path)
	})

	t.Run("fails on a failing pipeline under bash", func(t *testing.T) {
		assert.Error(t, runShell(t, Bash("false | true")))
	})

	t.Run("fails on an unset variable", func(t *testing.T) {
		assert.Error(t, runShell(t, Sh(`echo "$VIADUCT_DEFINITELY_UNSET"`)))
	})

	t.Run("carries on without strict mode", func(t *testing.T) {
		s := Bash("false\ntrue")
		s.NoStrict = true

		assert.NoError(t, runShell(t, s))
	})

	t.Run("runs in the working directory", func(t *testing.T) {
		dir := t.TempDir()

		s := Bash("touch here")
		s.WorkingDirectory = dir

		assert.NoError(t, runShell(t, s))
		assert.FileExists(t, filepath.Join(dir, "here"))
	})

	t.Run("skips when unless succeeds", func(t *testing.T) {
		s := Bash("false")
		s.Unless = "true"

		assert.NoError(t, runShell(t, s))
	})

	t.Run("runs unless in the working directory", func(t *testing.T) {
		dir := t.TempDir()
		assert.NoError(t, os.WriteFile(filepath.Join(dir, "done"), nil, 0o600))

		s := Bash("false")
		s.WorkingDirectory = dir
		s.Unless = "test -f done"

		assert.NoError(t, runShell(t, s))
	})

	t.Run("includes the cause of a failure", func(t *testing.T) {
		s := Bash("true")
		s.WorkingDirectory = filepath.Join(t.TempDir(), "missing")

		assert.ErrorContains(t, runShell(t, s), "no such file or directory")
	})

	t.Run("cleans up the script file", func(t *testing.T) {
		before, _ := filepath.Glob(filepath.Join(os.TempDir(), "viaduct-*.sh"))

		assert.NoError(t, runShell(t, Bash("true")))

		after, _ := filepath.Glob(filepath.Join(os.TempDir(), "viaduct-*.sh"))
		assert.ElementsMatch(t, before, after)
	})
}

func TestShellDescription(t *testing.T) {
	assert.Equal(t, "echo one", Bash("\n\n  echo one\n  echo two\n").Description())
	assert.Equal(t, "setup", (&Shell{Script: "echo one", Name: "setup"}).Description())
	assert.Equal(t, "apt-get update", Bash("#!/usr/bin/env bash\n# refresh\napt-get update").Description())
}

func TestShellLock(t *testing.T) {
	assert.False(t, Bash("true").Params().GlobalLock)
	assert.True(t, (&Shell{Script: "true", Lock: true}).Params().GlobalLock)

	keyed := (&Shell{Script: "true", LockKey: viaduct.PackageLock}).Params()
	assert.True(t, keyed.GlobalLock)
	assert.Equal(t, viaduct.PackageLock, keyed.LockKey)
}

func TestShellPreflightChecks(t *testing.T) {
	t.Run("requires a script", func(t *testing.T) {
		err := (&Shell{Script: "  \n"}).PreflightChecks(testLogger)
		assert.EqualError(t, err, "required parameter: Script")
	})

	t.Run("defaults to bash", func(t *testing.T) {
		s := &Shell{Script: "true"}
		assert.NoError(t, s.PreflightChecks(testLogger))
		assert.Equal(t, "bash", s.Interpreter)
	})

	t.Run("rejects other interpreters", func(t *testing.T) {
		err := (&Shell{Script: "true", Interpreter: "zsh"}).PreflightChecks(testLogger)
		assert.EqualError(t, err, `unsupported interpreter "zsh": use bash or sh`)
	})
}

func TestDedent(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"flush left", "a\nb", "a\nb\n"},
		{"common tabs", "\n\t\ta\n\t\t\tb\n\t", "a\n\tb\n"},
		{"common spaces", "  a\n    b\n", "a\n  b\n"},
		{"least indented line wins", "    a\n  b\n", "  a\nb\n"},
		{"blank lines inside", "\ta\n\n  \n\tb", "a\n\n\nb\n"},
		{"whitespace past the prefix kept", "\ta\n\t  \n\tb", "a\n  \nb\n"},
		{"mixed indent", "\ta\n  b", "\ta\n  b\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, dedent(tt.in))
		})
	}
}
