package viaduct

import (
	"context"
)

// Env is the environment resources run in. Apply puts it in each resource's
// context, and EnvFrom reads it back.
type Env struct {
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
