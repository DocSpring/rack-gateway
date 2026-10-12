package proxy

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// buildCreateParams are the form parameters a build create may carry: the archive url (Convox pkg/api
// controllers.go BuildCreate) and the options in BuildCreateOptions (pkg/structs/build.go), except external. Any
// other parameter is refused rather than forwarded unchecked.
var buildCreateParams = map[string]struct{}{
	"build-args": {}, "description": {}, "development": {}, "git-sha": {}, "manifest": {}, "no-cache": {},
	"url": {}, "wildcard-domain": {},
}

var buildArgName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// validateBuildRequest checks a build create before it's forwarded. original is the rack path it's forwarded to.
func (h *Handler) validateBuildRequest(
	r *http.Request,
	original string,
	bodyBytes []byte,
	authUser *auth.User,
) error {
	if r.Method != http.MethodPost || !rbac.KeyMatch3(original, "/apps/{app}/builds") {
		return nil
	}

	vals, err := url.ParseQuery(string(bodyBytes))
	if err != nil {
		return refuse(http.StatusBadRequest, errors.New("invalid build request body"))
	}
	if err := checkBuildForm(vals); err != nil {
		return refuse(http.StatusBadRequest, err)
	}

	if authUser.IsAPIToken {
		if tracker := getDeployApprovalTracker(r.Context()); tracker != nil && tracker.request != nil {
			return h.validateTokenBuildBinding(r, vals, tracker)
		}
	}
	return h.validateBuildManifestForAllUsers(r, extractAppFromPath(original), vals)
}

// checkBuildForm checks a build create's parameters before anything is read from the rack.
func checkBuildForm(vals url.Values) error {
	if err := checkBuildParams(vals); err != nil {
		return err
	}
	if err := refuseSurroundingWhitespace(vals, "url", "manifest"); err != nil {
		return err
	}
	return checkBuildArgs(vals.Get("build-args"))
}

// checkBuildParams refuses a build with a parameter the gateway doesn't accept, or one given more than once. An
// external build is refused for everyone: the rack answers it with a registry URL that holds the rack's own API
// password (provider/k8s/build.go), for the caller to push images to, and the gateway never hands out the rack's
// credentials.
func checkBuildParams(vals url.Values) error {
	if vals.Has("external") {
		return errors.New(
			"external builds aren't allowed through the gateway: the rack would answer with its own API credentials",
		)
	}
	var unsupported []string
	for name, values := range vals {
		if _, ok := buildCreateParams[name]; !ok {
			unsupported = append(unsupported, name)
		} else if len(values) > 1 {
			return fmt.Errorf("invalid build request: %s is given more than once", name)
		}
	}
	if len(unsupported) > 0 {
		sort.Strings(unsupported)
		return fmt.Errorf("unsupported build parameters: %s", strings.Join(unsupported, ", "))
	}
	return nil
}

// refuseSurroundingWhitespace refuses a build whose archive or manifest value starts or ends with whitespace. The
// rack uses these values exactly as sent, so "convox.yml " would be a different manifest (and a trailing space in
// the url a different object) from the one the gateway checked.
func refuseSurroundingWhitespace(vals url.Values, names ...string) error {
	for _, name := range names {
		if value := vals.Get(name); value != strings.TrimSpace(value) {
			return fmt.Errorf("invalid build request: %s must not start or end with whitespace", name)
		}
	}
	return nil
}

// checkBuildArgs refuses build arguments the rack would misuse. The rack splits build-args on commas and adds each
// to the build's command line, which it splits like a shell does (provider/k8s/build.go), so whitespace, a quote
// or a backslash in one would add build flags of its own. A NAME without "=value" takes its value from the build
// process's environment (pkg/build buildEnvs), which holds the rack's API password and registry credentials. And
// the build substitutes build arguments into the manifest's ${NAME} references (pkg/build/buildkit.go), where a
// line break or quote could change the manifest's structure.
func checkBuildArgs(buildArgs string) error {
	if buildArgs == "" {
		return nil
	}
	for _, arg := range strings.Split(buildArgs, ",") {
		name, value, ok := strings.Cut(arg, "=")
		if !ok || !buildArgName.MatchString(name) {
			return errors.New("invalid build argument: each must be NAME=value")
		}
		if strings.ContainsFunc(value, unsafeBuildArgRune) {
			return fmt.Errorf("invalid build argument %s: its value can't contain whitespace, quotes, "+
				"backslashes or control characters", name)
		}
	}
	return nil
}

func unsafeBuildArgRune(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsControl(r) || r == '"' || r == '\'' || r == '\\'
}
