package proxy

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// convoxManifest is the part of a convox.yml the gateway checks: the images its services and resources run.
type convoxManifest struct {
	Services  map[string]convoxService  `yaml:"services"`
	Resources map[string]convoxResource `yaml:"resources"`
}

// convoxService is one service's images. Without an image, the rack builds the service from the archive's source.
type convoxService struct {
	Image         string               `yaml:"image"`
	InitContainer *convoxInitContainer `yaml:"initContainer"`
}

// convoxResource is a resource's image override. Without one, the rack runs its default image for the type.
type convoxResource struct {
	Image string `yaml:"image"`
}

// convoxInitContainer is a service's init container. It gets the service's environment, and without an image of
// its own it runs the service's image (the rack's service template coalesces the two).
type convoxInitContainer struct {
	Image string `yaml:"image"`
}

// parseManifest parses a build manifest for checking. The YAML is parsed once, into a node tree: the raw text is
// checked for ${...} the rack would substitute, the tree's structure is checked (see checkManifestStructure), and
// the services are decoded from that same tree.
func parseManifest(data []byte) (*convoxManifest, error) {
	if err := checkInterpolation(data); err != nil {
		return nil, err
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("failed to parse manifest YAML: %w", err)
	}
	if err := checkManifestStructure(&root); err != nil {
		return nil, fmt.Errorf("manifest refused: %w", err)
	}

	var manifest convoxManifest
	if root.Kind == 0 {
		return &manifest, nil // an empty document: the policy refuses a manifest without services
	}
	if err := root.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest YAML: %w", err)
	}
	return &manifest, nil
}
