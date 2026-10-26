package audit

import (
	"context"

	"github.com/DocSpring/rack-gateway/internal/cmdenv"
)

// CommandEnv is the environment the command a request runs gets (an exec's, or a process start's), which the
// request's audit entries record (see BuildDetailsJSON).
type CommandEnv struct {
	// Plan is the env vars the command gets.
	Plan cmdenv.Plan
	// Write is set when the command was given the real values of the app's read-only env vars.
	Write bool
}

type commandEnvContextKey struct{}

// WithCommandEnv returns ctx carrying the environment of the command its request runs.
func WithCommandEnv(ctx context.Context, env CommandEnv) context.Context {
	return context.WithValue(ctx, commandEnvContextKey{}, env)
}

// CommandEnvFromContext returns the environment WithCommandEnv gave ctx, and whether it gave one.
func CommandEnvFromContext(ctx context.Context) (CommandEnv, bool) {
	env, ok := ctx.Value(commandEnvContextKey{}).(CommandEnv)
	return env, ok
}

// details are the audit details that record the environment: all_env_vars when the command got every env var
// (except RACK_URL), or env_vars, the names it got (only those that were set reached it); env_read_only, each
// read-only var it got with the var it took its value from; and write when it was given their real values.
func (e CommandEnv) details() map[string]interface{} {
	details := map[string]interface{}{}
	if e.Plan.All {
		details["all_env_vars"] = true
	} else {
		details["env_vars"] = append([]string{}, e.Plan.Vars...)
	}
	if len(e.Plan.ReadOnly) > 0 {
		details["env_read_only"] = e.Plan.ReadOnly
	}
	if e.Write {
		details["write"] = true
	}
	return details
}
