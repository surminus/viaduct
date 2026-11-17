package viaduct

import (
	"context"
	"os"
	"path/filepath"
)

// Env is the environment resources run in. Apply puts it in each resource's
// context, and EnvFrom reads it back.
type Env struct {
	// DryRun makes resources check everything they would change, and log
	// what they would do, without changing anything.
	DryRun bool

	// Root is the directory every absolute path a resource reads or writes
	// itself is taken to be under, including /proc and /sys. Paths given to
	// commands are not changed, so Apply needs a Runner alongside it. Root
	// privilege checks pass when it is set. The default is "/".
	Root string

	// Runner runs the commands resources need. The default is ExecRunner.
	Runner Runner

	// BlockDevice reports whether an absolute path that exists is a block
	// device. The default checks the file's mode under Root.
	BlockDevice func(path string) bool
}

type envKey struct{}

// WithEnv returns a context carrying the environment.
func WithEnv(ctx context.Context, env Env) context.Context {
	return context.WithValue(ctx, envKey{}, env)
}

// EnvFrom returns the environment carried by the context, or the default
// environment if it carries none.
func EnvFrom(ctx context.Context) Env {
	env, _ := ctx.Value(envKey{}).(Env)

	if env.Runner == nil {
		env.Runner = ExecRunner
	}

	return env
}

// isDryRun reports whether the context carries a dry run Env.
func isDryRun(ctx context.Context) bool {
	env, _ := ctx.Value(envKey{}).(Env)
	return env.DryRun
}

// Path returns where an absolute path is found, under Root when it is set.
// ".." cannot climb above Root: "/../etc" is Root's etc. A relative path is
// returned as it is.
func (e Env) Path(path string) string {
	if e.Root == "" || !filepath.IsAbs(path) {
		return path
	}

	// Cleaning against "/" first resolves every ".." before Root is added.
	return filepath.Join(e.Root, filepath.Clean("/"+path))
}

// IsRoot reports whether resources have root privileges. It is always true
// when Root is set.
func (e Env) IsRoot() bool {
	return e.Root != "" || IsRoot()
}

// IsBlockDevice reports whether an absolute path is a block device, using
// BlockDevice when it is set. It returns an error when the path does not
// exist or cannot be read.
func (e Env) IsBlockDevice(path string) (bool, error) {
	info, err := os.Stat(e.Path(path))
	if err != nil {
		return false, err
	}

	if e.BlockDevice != nil {
		return e.BlockDevice(path), nil
	}

	return info.Mode()&os.ModeDevice != 0 && info.Mode()&os.ModeCharDevice == 0, nil
}

// ResolveSymlink is ResolveSymlink for a path a resource reads or writes. It
// applies Path, and with Root set, an absolute symlink is resolved from Root
// rather than from "/", and ".." cannot climb above it.
func (e Env) ResolveSymlink(path string) (string, error) {
	if e.Root == "" || !filepath.IsAbs(path) {
		return ResolveSymlink(path)
	}

	return resolveIn(e.Root, e.Path(path))
}
