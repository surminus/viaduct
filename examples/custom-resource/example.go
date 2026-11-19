// Package example is a custom resource with everything a resource needs: it
// checks the current state, does nothing when it is already right, reports
// the change it makes, skips it in a dry run, and runs commands through
// viaduct so they are logged and can be faked in tests.
package example

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/surminus/viaduct"
)

// Motd writes a message of the day, then runs a command to refresh anything
// that caches it.
type Motd struct {
	// Message is the content of /etc/motd.
	Message string

	// Refresh is a command to run after the message changes. Optional.
	Refresh []string
}

// Description identifies this resource in logs and results.
func (m *Motd) Description() string {
	return "/etc/motd"
}

// OperationName says what the resource does, such as Create or Update.
func (m *Motd) OperationName() string {
	return "Update"
}

// Params says how the resource interacts with the run, such as which lock it
// needs or how long it may take.
func (m *Motd) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParams()
}

// PreflightChecks validates the resource and sets defaults. It runs for every
// resource before any of them runs.
func (m *Motd) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
	if m.Message == "" {
		return fmt.Errorf("required parameter: Message")
	}

	if !strings.HasSuffix(m.Message, "\n") {
		m.Message += "\n"
	}

	return nil
}

// Run makes the change.
func (m *Motd) Run(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)

	// Paths go through Env.Path, so tests can run against a directory
	// Resolved under the environment's root, so a symlink there cannot lead
	// out of it
	path, err := env.ResolveSymlink("/etc/motd")
	if err != nil {
		return err
	}

	current, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	// Already right: report a noop and change nothing
	if string(current) == m.Message {
		log.Noop("up-to-date", "path", "/etc/motd")
		return nil
	}

	// Report the change, then stop here in a dry run
	log.Info("updated", "path", "/etc/motd")

	if env.DryRun {
		return nil
	}

	if err := viaduct.WriteFileAtomic(path, []byte(m.Message), 0o644); err != nil {
		return err
	}

	if len(m.Refresh) == 0 {
		return nil
	}

	// Commands go through the environment's Runner, and their output is
	// logged line by line
	return viaduct.Command{Name: m.Refresh[0], Args: m.Refresh[1:]}.Run(ctx, log)
}
