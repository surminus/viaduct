package viaduct

import (
	"context"
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
