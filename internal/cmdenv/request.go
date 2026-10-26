package cmdenv

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// The headers that carry what a command asks for, from the CLI to the gateway (exec and run's --env,
// --all-env-vars and --write). The gateway reads them on an exec or a process start and doesn't forward them to the
// rack.
const (
	// EnvHeader names the env vars the command asks for, comma-separated.
	EnvHeader = "X-Command-Env"
	// AllEnvVarsHeader is "true" when the command asks for every env var.
	AllEnvVarsHeader = "X-Command-All-Env-Vars"
	// WriteHeader is "true" when the command asks for the real values of the app's read-only env vars.
	WriteHeader = "X-Command-Write"
)

// Request is what a command asks for: env vars besides the app's, every env var, or the real values of the read-only
// ones.
type Request struct {
	// Env names the env vars the command asks for besides the ones every command in the app gets.
	Env []string
	// AllEnvVars asks for every env var (except RACK_URL).
	AllEnvVars bool
	// Write asks for the real values of the app's read-only env vars, so the command can change data.
	Write bool
}

// IsZero reports whether r asks for nothing besides what every command in the app gets.
func (r Request) IsZero() bool {
	return len(r.Env) == 0 && !r.AllEnvVars && !r.Write
}

var errEnvAndAll = errors.New("--all-env-vars gives the command every env var, so it can't name some with --env too")

// Check refuses a request with a name that isn't a valid env var name, RACK_URL, or names given with every env var.
func (r Request) Check() error {
	if err := CheckNames(r.Env); err != nil {
		return err
	}
	if r.AllEnvVars && len(r.Env) > 0 {
		return errEnvAndAll
	}
	return nil
}

// Headers returns the headers that carry r.
func (r Request) Headers() map[string]string {
	headers := map[string]string{}
	if len(r.Env) > 0 {
		headers[EnvHeader] = strings.Join(r.Env, ",")
	}
	if r.AllEnvVars {
		headers[AllEnvVarsHeader] = "true"
	}
	if r.Write {
		headers[WriteHeader] = "true"
	}
	return headers
}

// RequestFrom reads the request header carries, checked (see Request.Check). Each header may be given once, and the
// flags only as "true".
func RequestFrom(header http.Header) (Request, error) {
	var r Request
	env, err := single(header, EnvHeader)
	if err != nil {
		return Request{}, err
	}
	if env != "" {
		for _, name := range strings.Split(env, ",") {
			r.Env = append(r.Env, strings.TrimSpace(name))
		}
	}
	if r.AllEnvVars, err = flag(header, AllEnvVarsHeader); err != nil {
		return Request{}, err
	}
	if r.Write, err = flag(header, WriteHeader); err != nil {
		return Request{}, err
	}
	return r, r.Check()
}

// single returns the one value of header name, or "" when it isn't given.
func single(header http.Header, name string) (string, error) {
	values := header.Values(name)
	if len(values) > 1 {
		return "", fmt.Errorf("the request gives %s more than once", name)
	}
	if len(values) == 0 {
		return "", nil
	}
	return strings.TrimSpace(values[0]), nil
}

// flag returns whether header name is given, as "true".
func flag(header http.Header, name string) (bool, error) {
	value, err := single(header, name)
	if err != nil || value == "" {
		return false, err
	}
	if value != "true" {
		return false, fmt.Errorf("%s must be true, got %q", name, value)
	}
	return true, nil
}
