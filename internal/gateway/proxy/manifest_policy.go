package proxy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// imagePolicy describes which images a build manifest may reference.
type imagePolicy struct {
	// commit, when set, requires every service to use a pre-built image tagged with this full commit SHA.
	commit string
	// patterns are the app's service_image_patterns with {{GIT_COMMIT}} already substituted.
	patterns map[string]string
}

// imagePattern is a compiled service image pattern, with its source for error messages.
type imagePattern struct {
	re     *regexp.Regexp
	source string
}

// newImagePolicy builds a policy from the app's pattern templates. When requireCommitTag is true, every
// service image must also be tagged with commit.
func newImagePolicy(patternTemplates map[string]string, commit string, requireCommitTag bool) imagePolicy {
	policy := imagePolicy{patterns: substituteCommit(patternTemplates, commit)}
	if requireCommitTag {
		policy.commit = commit
	}
	return policy
}

// validate checks every image the manifest's services run: each service's image, and its init container's when it
// sets one (without one, the init container runs the service's image). A service's init container is held to the
// service's pattern. checkManifestStructure has already refused image keys anywhere else.
func (p imagePolicy) validate(manifest *convoxManifest) error {
	if manifest == nil || len(manifest.Services) == 0 {
		return fmt.Errorf("no services defined in manifest")
	}
	compiled, err := compileImagePatterns(p.patterns)
	if err != nil {
		return err
	}
	for _, name := range sortedServiceNames(manifest) {
		if err := p.validateService(name, manifest.Services[name], compiled); err != nil {
			return err
		}
	}
	return nil
}

// compileImagePatterns compiles each pattern anchored, so it must match the whole image reference.
func compileImagePatterns(patterns map[string]string) (map[string]imagePattern, error) {
	compiled := make(map[string]imagePattern, len(patterns))
	for service, pattern := range patterns {
		re, err := regexp.Compile("^(?:" + pattern + ")$")
		if err != nil {
			return nil, fmt.Errorf("invalid image pattern for service %s: %w", service, err)
		}
		compiled[service] = imagePattern{re: re, source: pattern}
	}
	return compiled, nil
}

func (p imagePolicy) validateService(name string, service convoxService, compiled map[string]imagePattern) error {
	pattern, hasPattern := compiled[name]
	if !hasPattern {
		// The wildcard pattern applies to services without their own pattern (lowest precedence).
		pattern, hasPattern = compiled["*"]
	}
	if !hasPattern && p.commit == "" {
		return nil // no pattern applies, so the service's images aren't restricted
	}

	if strings.TrimSpace(service.Image) == "" {
		// A service built from source in the uploaded archive can't be tied to a commit or a pattern.
		if p.commit != "" {
			return fmt.Errorf(
				"service %s must use a pre-built image tagged with the approved commit %s", name, p.commit,
			)
		}
		return fmt.Errorf("service %s must use a pre-built image (image pattern is configured for this service)", name)
	}
	if !hasPattern {
		// The commit tag doesn't pin the repository, so every service needs a pattern that does.
		return fmt.Errorf(
			`service %s has no image pattern; add one for it or a "*" pattern to service_image_patterns`, name,
		)
	}

	if err := p.validateImage("service "+name, service.Image, pattern); err != nil {
		return err
	}
	if service.InitContainer != nil && service.InitContainer.Image != "" {
		return p.validateImage("service "+name+" init container", service.InitContainer.Image, pattern)
	}
	return nil
}

// validateImage checks one image reference against the commit (when the policy has one) and the pattern.
func (p imagePolicy) validateImage(owner, image string, pattern imagePattern) error {
	// The rack substitutes ${...} after the gateway has checked the manifest, so a checked image must be literal.
	if strings.Contains(image, "${") {
		return fmt.Errorf("%s image %q uses ${...}, which the rack substitutes after the check", owner, image)
	}
	if p.commit != "" && !imageTaggedWithCommit(image, p.commit) {
		return fmt.Errorf("%s image %q is not tagged with the approved commit %s", owner, image, p.commit)
	}
	if !pattern.re.MatchString(image) {
		return fmt.Errorf("%s image %q does not match required pattern %q", owner, image, pattern.source)
	}
	return nil
}

// substituteCommit replaces {{GIT_COMMIT}} in each pattern with the regex-escaped commit.
func substituteCommit(templates map[string]string, commit string) map[string]string {
	patterns := make(map[string]string, len(templates))
	for service, template := range templates {
		patterns[service] = strings.ReplaceAll(template, "{{GIT_COMMIT}}", regexp.QuoteMeta(commit))
	}
	return patterns
}

// imageTaggedWithCommit reports whether an image reference's tag is commit or starts with commit+"-".
func imageTaggedWithCommit(image, commit string) bool {
	ref := image
	if at := strings.Index(ref, "@"); at >= 0 {
		ref = ref[:at]
	}
	colon := strings.LastIndex(ref, ":")
	if colon <= strings.LastIndex(ref, "/") {
		return false // no tag (a colon before the last slash is a registry port)
	}
	tag := strings.ToLower(ref[colon+1:])
	commit = strings.ToLower(commit)
	return tag == commit || strings.HasPrefix(tag, commit+"-")
}

func sortedServiceNames(manifest *convoxManifest) []string {
	names := make([]string, 0, len(manifest.Services))
	for name := range manifest.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
