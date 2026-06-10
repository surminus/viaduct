package resources

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/surminus/viaduct"
)

// Line manages a single line within a file, for editing files that are not
// fully managed by a File resource. If the file does not exist it is
// created containing the line.
type Line struct {
	// Path is the file to manage
	Path string

	// Line is the line that should be present in the file
	Line string

	// Match is a regular expression. If a line matches, it is replaced
	// with Line rather than Line being appended. If multiple lines
	// match, the first is replaced and the rest are removed. Optional.
	Match string

	// Delete removes lines rather than adding them. Lines are removed
	// if they match the Match expression, or are equal to Line if Match
	// is not set.
	Delete bool

	// regex is the compiled Match expression
	regex *regexp.Regexp
}

// AppendLine ensures the line exists in the file
func AppendLine(path, line string) *Line {
	return &Line{Path: path, Line: line}
}

// ReplaceLine replaces lines matching the expression with line, appending
// it if there is no match
func ReplaceLine(path, match, line string) *Line {
	return &Line{Path: path, Match: match, Line: line}
}

// DeleteLine removes lines matching the expression from the file
func DeleteLine(path, match string) *Line {
	return &Line{Path: path, Match: match, Delete: true}
}

func (l *Line) Description() string {
	return l.Path
}

func (l *Line) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParams()
}

// PreflightChecks sets default values for the parameters for a particular
// resource
func (l *Line) PreflightChecks(log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if l.Path == "" {
		return fmt.Errorf("required parameter: Path")
	}

	if l.Delete {
		if l.Line == "" && l.Match == "" {
			return fmt.Errorf("delete requires one of Line or Match")
		}
	} else {
		if l.Line == "" {
			return fmt.Errorf("required parameter: Line")
		}
	}

	if l.Match != "" {
		regex, err := regexp.Compile(l.Match)
		if err != nil {
			return fmt.Errorf("invalid Match expression: %s", err)
		}

		l.regex = regex
	}

	return nil
}

func (l *Line) OperationName() string {
	if l.Delete {
		return "Delete"
	}

	return "Update"
}

func (l *Line) Run(log *viaduct.Logger) error {
	if l.Delete {
		return l.deleteLine(log)
	}

	return l.updateLine(log)
}

func (l *Line) updateLine(log *viaduct.Logger) error {
	path := viaduct.ExpandPath(l.Path)

	if viaduct.Cli.DryRun {
		log.Info("updated", "path", path, "line", l.Line)
		return nil
	}

	if !viaduct.FileExists(path) {
		if err := writeLines(path, []string{l.Line}); err != nil {
			return err
		}

		log.Info("created", "path", path, "line", l.Line)
		return nil
	}

	lines, err := readLines(path)
	if err != nil {
		return err
	}

	var out []string
	var changed, replaced bool

	for _, line := range lines {
		if l.regex != nil && l.regex.MatchString(line) {
			// Replace the first match, and remove any others
			if replaced {
				changed = true
				continue
			}

			replaced = true
			if line != l.Line {
				changed = true
			}

			out = append(out, l.Line)
		} else {
			out = append(out, line)
		}
	}

	if !replaced {
		if slices.Contains(out, l.Line) {
			log.Noop("up-to-date", "path", path, "line", l.Line)
			return nil
		}

		out = append(out, l.Line)
		changed = true
	}

	if !changed {
		log.Noop("up-to-date", "path", path, "line", l.Line)
		return nil
	}

	if err := writeLines(path, out); err != nil {
		return err
	}

	log.Info("updated", "path", path, "line", l.Line)

	return nil
}

func (l *Line) deleteLine(log *viaduct.Logger) error {
	path := viaduct.ExpandPath(l.Path)

	if viaduct.Cli.DryRun {
		log.Info("deleted", "path", path)
		return nil
	}

	if !viaduct.FileExists(path) {
		log.Noop("up-to-date", "path", path)
		return nil
	}

	lines, err := readLines(path)
	if err != nil {
		return err
	}

	var out []string
	for _, line := range lines {
		if l.regex != nil {
			if l.regex.MatchString(line) {
				continue
			}
		} else if line == l.Line {
			continue
		}

		out = append(out, line)
	}

	if len(out) == len(lines) {
		log.Noop("up-to-date", "path", path)
		return nil
	}

	if err := writeLines(path, out); err != nil {
		return err
	}

	log.Info("deleted", "path", path)

	return nil
}

func readLines(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if len(content) == 0 {
		return nil, nil
	}

	return strings.Split(strings.TrimSuffix(string(content), "\n"), "\n"), nil
}

func writeLines(path string, lines []string) error {
	// Preserve the mode of an existing file
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode()
	}

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), mode)
}
