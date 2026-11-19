package resources

import (
	"context"
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

	// Substitute replaces the text Match matches within each line with
	// Line, like sed's s///g, rather than replacing whole lines. Line can
	// refer to groups in Match with $1 or ${name}, and can be empty to remove
	// the matched text. Nothing is appended, and a missing file is left
	// alone. Requires Match.
	Substitute bool `json:",omitempty"`

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

// Substitute replaces the text matching the expression within each line of
// the file with replacement
func Substitute(path, match, replacement string) *Line {
	return &Line{Path: path, Match: match, Line: replacement, Substitute: true}
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
func (l *Line) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if l.Path == "" {
		return fmt.Errorf("required parameter: Path")
	}

	switch {
	case l.Substitute:
		if l.Delete {
			return fmt.Errorf("cannot set both Delete and Substitute")
		}

		if l.Match == "" {
			return fmt.Errorf("substitute requires Match")
		}
	case l.Delete:
		if l.Line == "" && l.Match == "" {
			return fmt.Errorf("delete requires one of Line or Match")
		}
	default:
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

func (l *Line) Run(ctx context.Context, log *viaduct.Logger) error {
	switch {
	case l.Substitute:
		return l.substitute(ctx, log)
	case l.Delete:
		return l.deleteLine(ctx, log)
	default:
		return l.updateLine(ctx, log)
	}
}

func (l *Line) substitute(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)
	path, err := env.ResolveSymlink(viaduct.ExpandPath(l.Path))
	if err != nil {
		return err
	}

	defer lockPath(path)()

	if !viaduct.FileExists(path) {
		log.Noop("up-to-date", "path", path)
		return nil
	}

	lines, err := readLines(path)
	if err != nil {
		return err
	}

	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = l.regex.ReplaceAllString(line, l.Line)
	}

	if slices.Equal(out, lines) {
		log.Noop("up-to-date", "path", path)
		return nil
	}

	if !change(ctx, log, "substituted", "path", path) {
		return nil
	}

	return writeLines(path, out)
}

func (l *Line) updateLine(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)
	path, err := env.ResolveSymlink(viaduct.ExpandPath(l.Path))
	if err != nil {
		return err
	}

	// Serialise edits to this file so a concurrent Line resource on the
	// same path can't clobber our read-modify-write.
	defer lockPath(path)()

	if !viaduct.FileExists(path) {
		if !change(ctx, log, "created", "path", path, "line", l.Line) {
			return nil
		}

		return writeLines(path, []string{l.Line})
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

	if !change(ctx, log, "updated", "path", path, "line", l.Line) {
		return nil
	}

	return writeLines(path, out)
}

func (l *Line) deleteLine(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)
	path, err := env.ResolveSymlink(viaduct.ExpandPath(l.Path))
	if err != nil {
		return err
	}

	// Serialise edits to this file so a concurrent Line resource on the
	// same path can't clobber our read-modify-write.
	defer lockPath(path)()

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

	if !change(ctx, log, "deleted", "path", path) {
		return nil
	}

	return writeLines(path, out)
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

// writeLines replaces a file with the lines, keeping an existing file's mode
// and ownership
func writeLines(path string, lines []string) error {
	return viaduct.WriteFileAtomic(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}
