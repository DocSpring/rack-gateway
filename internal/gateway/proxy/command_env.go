package proxy

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/DocSpring/rack-gateway/internal/cmdenv"
	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// errApprovedCommandEnv refuses a command under a deploy approval that asks for env vars of its own.
var errApprovedCommandEnv = errors.New("under a deploy approval, the command's entry in approved_deploy_commands " +
	"decides its env vars: run it without --env, --all-env-vars and --write")

// errEnvUndecided refuses to forward a command whose env vars weren't decided (checkCommandEnv): the gateway never
// runs a command with env vars nothing chose.
var errEnvUndecided = errors.New("the command's env vars weren't decided, so it isn't run")

// checkCommandEnv decides the env vars of the command r runs (an exec's, or a process start's) and puts them on r,
// for the command forwarded to the rack (wrapCommand) and the request's audit entries. The command gets the app's
// command_env_vars and the ones it asks for (cmdenv.Request), or every env var, with the read-only vars' substitutes
// unless it asks to write. Under a deploy approval its entry in approved_deploy_commands asks instead, and a command
// that asks for anything itself is refused; a command that isn't approved is left undecided, and refused before
// it's forwarded. `convox run`'s placeholder gets no env vars at all: it only sleeps, and it needs no MFA proof.
func (h *Handler) checkCommandEnv(r *http.Request, tracker *deployApprovalTracker) error {
	command, ok := audit.RequestCommand(r)
	if !ok {
		return nil
	}
	if len(r.Header.Values("Command")) > 1 {
		return refuse(http.StatusBadRequest, errors.New("the request gives more than one command"))
	}
	req, err := cmdenv.RequestFrom(r.Header)
	if err != nil {
		return refuse(http.StatusBadRequest, err)
	}
	if err := cmdenv.CheckCommand(command); err != nil {
		return refuse(http.StatusBadRequest, err)
	}
	if hasApproval(tracker) && !req.IsZero() {
		return refuse(http.StatusBadRequest, errApprovedCommandEnv)
	}

	if isAttachedRunPlaceholder(r) {
		*r = *r.WithContext(audit.WithCommandEnv(r.Context(), audit.CommandEnv{}))
		return nil
	}
	app := extractAppFromPath(r.URL.Path)
	if hasApproval(tracker) {
		approved, ok := h.approvedCommand(app, command)
		if !ok {
			return nil
		}
		req = approved.Request()
	}
	plan, err := h.commandPlan(app, req)
	if err != nil {
		return err
	}
	*r = *r.WithContext(audit.WithCommandEnv(r.Context(), audit.CommandEnv{Plan: plan, Write: req.Write}))
	return nil
}

// commandPlan returns the env vars a command in app that asks for req gets (settings.CommandEnv.Plan).
func (h *Handler) commandPlan(app string, req cmdenv.Request) (cmdenv.Plan, error) {
	var env settings.CommandEnv
	if h.settingsService != nil {
		var err error
		if env, err = h.settingsService.GetCommandEnv(app); err != nil {
			return cmdenv.Plan{}, fmt.Errorf("failed to read the env vars of %s's commands: %w", app, err)
		}
	}
	plan, err := env.Plan(req)
	if err != nil {
		return cmdenv.Plan{}, refuse(http.StatusBadRequest, err)
	}
	return plan, nil
}

// wrapCommand replaces the command in header, the headers forwarded to the rack for r, with the wrapper that runs it
// with the env vars checkCommandEnv decided (cmdenv.Build). The audit log, the deploy approval checks and the
// approval's record all read the command r carries, as it was sent.
func wrapCommand(r *http.Request, header http.Header) error {
	command, ok := audit.RequestCommand(r)
	if !ok {
		return nil
	}
	env, decided := audit.CommandEnvFromContext(r.Context())
	if !decided {
		return refuse(http.StatusForbidden, errEnvUndecided)
	}
	header.Set("Command", cmdenv.Build(env.Plan, command))
	return nil
}
