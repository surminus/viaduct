package resources

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/surminus/viaduct"
)

type Shell struct {
	// Script is the shell script to run. It can span multiple lines, and the
	// indentation common to every line is removed before it runs, so it can
	// be written as an indented raw string. Required.
	Script string

	// Name describes the script in the logs. The default is the first line
	// of the script.
	Name string

	// Interpreter is the shell that runs the script: "bash" or "sh". The
	// default is "bash".
	Interpreter string

	// NoStrict runs the script without strict mode. By default the script
	// stops at the first failing command or unset variable, and under bash a
	// failure anywhere in a pipeline fails the pipeline.
	NoStrict bool

	// WorkingDirectory is where to run the script. Optional.
	WorkingDirectory string

	// Unless is a command to run first, which if it exits cleanly means the
	// script does not run. It runs through bash, in WorkingDirectory.
	// Optional.
	Unless string

	// Lock ensures the script does not run at the same time as other
	// resources holding a lock, such as Package. Optional.
	Lock bool

	// LockKey narrows the lock to a single domain, such as
	// viaduct.PackageLock, so the script only waits for other resources using
	// the same key. Implies Lock. Optional.
	LockKey string
}

// Bash runs a script with bash
func Bash(script string) *Shell {
	return &Shell{Script: script, Interpreter: "bash"}
}

// Sh runs a script with sh
func Sh(script string) *Shell {
	return &Shell{Script: script, Interpreter: "sh"}
}

func (s *Shell) Description() string {
	if s.Name != "" {
		return s.Name
	}

	// Skip comments, so a shebang or header comment does not stand in for
	// what the script does
	for line := range strings.SplitSeq(s.Script, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}

	return ""
}

func (s *Shell) Params() *viaduct.ResourceParams {
	return &viaduct.ResourceParams{GlobalLock: s.Lock || s.LockKey != "", LockKey: s.LockKey}
}

func (s *Shell) PreflightChecks(log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if strings.TrimSpace(s.Script) == "" {
		return fmt.Errorf("required parameter: Script")
	}

	// Set optional defaults here
	if s.Interpreter == "" {
		s.Interpreter = "bash"
	}

	if s.Interpreter != "bash" && s.Interpreter != "sh" {
		return fmt.Errorf("unsupported interpreter %q: use bash or sh", s.Interpreter)
	}

	s.Script = dedent(s.Script)

	return nil
}

func (s *Shell) OperationName() string {
	return "Run"
}

func (s *Shell) Run(log *viaduct.Logger) error {
	if unlessSucceeds(s.Unless, s.WorkingDirectory) {
		log.Noop("skipped", "script", s.Description())
		return nil
	}

	log.Info("started", "script", s.Description())
	if viaduct.Cli.DryRun {
		return nil
	}

	// The script goes in a file rather than "bash -c", which has a size limit
	// on the argument, or stdin, which any command in the script reading
	// stdin would consume. CreateTemp makes the file 0600 under a random
	// name, and running it through the interpreter means it needs no execute
	// bit and works when the temp directory is mounted noexec.
	f, err := os.CreateTemp("", "viaduct-*.sh")
	if err != nil {
		return fmt.Errorf("creating script file: %w", err)
	}
	defer os.Remove(f.Name())

	if _, err := f.WriteString(s.Script); err != nil {
		f.Close()
		return fmt.Errorf("writing script file: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("writing script file: %w", err)
	}

	cmd := exec.Command(s.Interpreter, append(s.flags(), f.Name())...) // nolint:gosec
	setCommandOutput(cmd)
	cmd.Dir = s.WorkingDirectory

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("script failed: %s: %w", s.Description(), err)
	}
	log.Info("finished", "script", s.Description())

	return nil
}

// flags returns the interpreter options for strict mode. They are passed on
// the command line rather than written into the script so that line numbers
// in error messages match the script as written. pipefail is bash only, since
// not every sh supports it.
func (s *Shell) flags() []string {
	if s.NoStrict {
		return nil
	}

	if s.Interpreter == "bash" {
		return []string{"-eu", "-o", "pipefail"}
	}

	return []string{"-eu"}
}

// dedent removes the leading whitespace common to every non-blank line, along
// with blank lines at the start and end, so a script written as an indented
// raw string runs as if it were written flush left. This matters for heredocs,
// whose terminator has to start the line.
func dedent(script string) string {
	lines := strings.Split(script, "\n")

	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}

	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	// lines[0] is non-blank here unless lines is empty, so its indent is the
	// starting candidate, cut back to whatever every later line shares
	var prefix string
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if i == 0 {
			prefix = indent
			continue
		}

		for !strings.HasPrefix(indent, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}

	for i, line := range lines {
		// Blank lines don't count towards the prefix, so one may be shorter
		// than it. Those are emptied, and the rest trimmed like any other
		// line so whitespace inside a heredoc survives.
		if !strings.HasPrefix(line, prefix) {
			lines[i] = ""
			continue
		}

		lines[i] = strings.TrimPrefix(line, prefix)
	}

	return strings.Join(lines, "\n") + "\n"
}
