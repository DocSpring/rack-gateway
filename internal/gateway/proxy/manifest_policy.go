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

// validate checks every image a build's manifest runs: each service's image, and its init container's when it sets
// one (without one, the init container runs the service's image). A service's init container is held to the
// service's pattern. A resource may not override its image.
func (p imagePolicy) validate(manifest *convoxManifest) error {
	if manifest == nil || len(manifest.Services) == 0 {
		return fmt.Errorf("no services defined in manifest")
	}
	if err := refuseResourceImages(manifest); err != nil {
		return err
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

// validateReleaseImages checks the images a promote takes from the release's manifest: init container images and
// resource images. Each service itself runs the image its build pushed to the app's registry
// (<repository>:<service>.<build>), whatever the manifest's image says now, so only these come from the manifest.
func (p imagePolicy) validateReleaseImages(manifest *convoxManifest) error {
	if err := refuseResourceImages(manifest); err != nil {
		return err
	}
	compiled, err := compileImagePatterns(p.patterns)
	if err != nil {
		return err
	}
	for _, name := range sortedServiceNames(manifest) {
		init := manifest.Services[name].InitContainer
		if init == nil || init.Image == "" {
			continue // the init container runs the service's own image
		}
		pattern, hasPattern := patternFor(name, compiled)
		if !hasPattern && p.commit == "" {
			continue // no pattern applies, so the service's images aren't restricted
		}
		if !hasPattern {
			return noPatternError(name)
		}
		if err := p.validateImage("service "+name+" init container", init.Image, pattern); err != nil {
			return err
		}
	}
	return nil
}

// refuseResourceImages refuses a resource that overrides its image: the rack would run it in place of the
// resource type's default image, and no pattern covers resources.
func refuseResourceImages(manifest *convoxManifest) error {
	names := make([]string, 0, len(manifest.Resources))
	for name := range manifest.Resources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if image := manifest.Resources[name].Image; image != "" {
			return fmt.Errorf("resource %s overrides its image (%q), which isn't allowed", name, image)
		}
	}
	return nil
}

// noPatternError refuses a service with no pattern where its images must be tied to a commit: the commit tag
// doesn't pin the repository, so every service needs a pattern that does.
func noPatternError(service string) error {
	return fmt.Errorf(
		`service %s has no image pattern; add one for it or a "*" pattern to service_image_patterns`, service,
	)
}

// patternFor returns the pattern for a service: its own, or the wildcard pattern (lowest precedence).
func patternFor(service string, compiled map[string]imagePattern) (imagePattern, bool) {
	if pattern, ok := compiled[service]; ok {
		return pattern, true
	}
	pattern, ok := compiled["*"]
	return pattern, ok
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
	pattern, hasPattern := patternFor(name, compiled)
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
		return noPatternError(name)
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
