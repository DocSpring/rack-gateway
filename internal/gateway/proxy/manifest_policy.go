package proxy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// imagePolicy describes which service images a build manifest may reference.
type imagePolicy struct {
	// commit, when set, requires every service to use a pre-built image tagged with this full commit SHA.
	commit string
	// patterns are the app's service_image_patterns with {{GIT_COMMIT}} already substituted.
	patterns map[string]string
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

func (p imagePolicy) validate(manifest *convoxManifest) error {
	if p.commit != "" {
		if err := validateImagesTaggedWithCommit(manifest, p.commit); err != nil {
			return err
		}
		// The commit tag doesn't pin the repository, so every service needs a pattern that does.
		if err := requirePatternForEveryService(manifest, p.patterns); err != nil {
			return err
		}
	}
	if len(p.patterns) == 0 {
		return nil
	}
	return validateServiceImages(manifest, p.patterns)
}

// requirePatternForEveryService fails when a service has neither its own pattern nor a "*" pattern.
func requirePatternForEveryService(manifest *convoxManifest, patterns map[string]string) error {
	if _, ok := patterns["*"]; ok {
		return nil
	}
	for _, name := range sortedServiceNames(manifest) {
		if _, ok := patterns[name]; !ok {
			return fmt.Errorf(
				`service %s has no image pattern; add one for it or a "*" pattern to service_image_patterns`,
				name,
			)
		}
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

// validateImagesTaggedWithCommit requires every service to use a pre-built image whose tag is the
// commit SHA, or starts with "<sha>-" (e.g. "<sha>-amd64"). Services built from source in the uploaded
// archive cannot be tied to a commit, so they are rejected.
func validateImagesTaggedWithCommit(manifest *convoxManifest, commit string) error {
	if manifest == nil || len(manifest.Services) == 0 {
		return fmt.Errorf("no services defined in manifest")
	}
	for _, name := range sortedServiceNames(manifest) {
		image := strings.TrimSpace(manifest.Services[name].Image)
		if image == "" {
			return fmt.Errorf(
				"service %s must use a pre-built image tagged with the approved commit %s",
				name,
				commit,
			)
		}
		if !imageTaggedWithCommit(image, commit) {
			return fmt.Errorf("service %s image %q is not tagged with the approved commit %s", name, image, commit)
		}
	}
	return nil
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

// validateServiceImages validates that all service images match their required patterns.
// Patterns are anchored, so they must match the whole image reference.
func validateServiceImages(manifest *convoxManifest, servicePatterns map[string]string) error {
	if manifest == nil || len(manifest.Services) == 0 {
		return fmt.Errorf("no services defined in manifest")
	}

	compiledPatterns := make(map[string]*regexp.Regexp, len(servicePatterns))
	for service, pattern := range servicePatterns {
		re, err := regexp.Compile("^(?:" + pattern + ")$")
		if err != nil {
			return fmt.Errorf("invalid image pattern for service %s: %w", service, err)
		}
		compiledPatterns[service] = re
	}

	for _, serviceName := range sortedServiceNames(manifest) {
		service := manifest.Services[serviceName]
		if err := validateServiceImage(serviceName, service, compiledPatterns, servicePatterns); err != nil {
			return err
		}
	}
	return nil
}

func validateServiceImage(
	serviceName string,
	service convoxService,
	compiled map[string]*regexp.Regexp,
	raw map[string]string,
) error {
	key := serviceName
	pattern, ok := compiled[key]
	if !ok {
		// Wildcard pattern applies to services without their own pattern (lowest precedence)
		key = "*"
		pattern, ok = compiled[key]
	}
	if !ok {
		return nil
	}
	if service.Image == "" {
		return fmt.Errorf(
			"service %s must use a pre-built image (image pattern is configured for this service)",
			serviceName,
		)
	}
	if !pattern.MatchString(service.Image) {
		return fmt.Errorf(
			"service %s image %q does not match required pattern %q",
			serviceName,
			service.Image,
			raw[key],
		)
	}
	return nil
}

func sortedServiceNames(manifest *convoxManifest) []string {
	names := make([]string, 0, len(manifest.Services))
	for name := range manifest.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
