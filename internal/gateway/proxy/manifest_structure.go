package proxy

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	imageKey      = "image"
	servicesKey   = "services"
	mergeKey      = "<<"
	yamlBinaryTag = "!!binary"
)

// checkManifestStructure walks a parsed manifest before its images are decoded. The gateway decodes only the image
// fields it checks, with yaml.v3; the rack decodes the whole manifest with yaml.v2 and its own ordered maps. The
// two agree on plain YAML but not on everything, so the walk refuses what could make them disagree, or run an image
// the gateway didn't check:
//   - an image key anywhere but a service's image and its init container's: a resource's image override, or any
//     image field the gateway doesn't know, could run an image nobody approved;
//   - merge keys (<<): yaml.v2 applies a merge in document order, so it can replace or add to keys written before
//     it (even add a second copy of a service), while yaml.v3 lets the explicit keys win;
//   - a key repeated in one mapping, however it's spelled: decoders disagree on which copy counts;
//   - an anchor on a node that holds an image key, so an alias never brings an image somewhere the walk doesn't
//     look (aliases aren't followed: what they name was walked where it was defined, and following them could
//     multiply the work).
func checkManifestStructure(root *yaml.Node) error {
	w := &manifestWalk{}
	_, err := w.node(root)
	return err
}

// manifestWalk is one walk over a manifest's nodes.
type manifestWalk struct {
	// path holds the keys from the document root to the current node ("-" for a sequence item). It's one stack
	// shared by the whole walk, so deep nesting costs no copies.
	path []string
}

// node checks n and everything under it, and reports whether that holds an image key.
func (w *manifestWalk) node(n *yaml.Node) (bool, error) {
	var holdsImage bool
	var err error
	switch n.Kind {
	case yaml.DocumentNode:
		holdsImage, err = w.items(n.Content)
	case yaml.SequenceNode:
		w.path = append(w.path, "-")
		holdsImage, err = w.items(n.Content)
		w.path = w.path[:len(w.path)-1]
	case yaml.MappingNode:
		holdsImage, err = w.mapping(n)
	}
	if err != nil {
		return false, err
	}
	if holdsImage && n.Anchor != "" {
		return false, fmt.Errorf(
			"line %d: anchor &%s holds an image key; write each image out where it's used", n.Line, n.Anchor,
		)
	}
	return holdsImage, nil
}

func (w *manifestWalk) items(items []*yaml.Node) (bool, error) {
	holdsImage := false
	for _, item := range items {
		held, err := w.node(item)
		if err != nil {
			return false, err
		}
		holdsImage = holdsImage || held
	}
	return holdsImage, nil
}

func (w *manifestWalk) mapping(n *yaml.Node) (bool, error) {
	seen := make(map[string]bool, len(n.Content)/2)
	holdsImage := false
	for i := 0; i+1 < len(n.Content); i += 2 {
		keyNode := n.Content[i]
		key, err := mappingKey(keyNode)
		if err != nil {
			return false, err
		}
		if seen[key] {
			return false, fmt.Errorf("line %d: key %q appears more than once in the same mapping", keyNode.Line, key)
		}
		seen[key] = true

		held, err := w.entry(key, keyNode.Line, n.Content[i+1])
		if err != nil {
			return false, err
		}
		holdsImage = holdsImage || held
	}
	return holdsImage, nil
}

// entry checks one key and its value.
func (w *manifestWalk) entry(key string, line int, value *yaml.Node) (bool, error) {
	w.path = append(w.path, key)
	defer func() { w.path = w.path[:len(w.path)-1] }()

	isImage := key == imageKey
	if isImage {
		if err := checkImageKeyPlacement(w.path, line); err != nil {
			return false, err
		}
	}
	held, err := w.node(value)
	if err != nil {
		return false, err
	}
	return isImage || held, nil
}

// mappingKey returns a mapping key as a decoder reads it. Most keys are their text, but an alias key reads as the
// node it names and a !!binary key as its decoded bytes, so "image" can be spelled other ways. Merge keys are
// refused (see checkManifestStructure).
func mappingKey(n *yaml.Node) (string, error) {
	key := n.Value
	if n.Kind != yaml.ScalarNode || n.ShortTag() == yamlBinaryTag {
		if err := n.Decode(&key); err != nil {
			return "", fmt.Errorf("line %d: unsupported mapping key: %w", n.Line, err)
		}
	}
	if key == mergeKey {
		return "", fmt.Errorf(
			"line %d: YAML merge keys (<<) aren't allowed: the rack's YAML parser applies them differently", n.Line,
		)
	}
	return key, nil
}

// checkImageKeyPlacement accepts an image key (the last element of path) only where the gateway checks it:
// services.<name>.image and services.<name>.initContainer.image. Timers have no image of their own (they run their
// service's), so every other image key is one the gateway doesn't check.
func checkImageKeyPlacement(path []string, line int) error {
	switch {
	case len(path) == 3 && path[0] == servicesKey:
		return nil
	case len(path) == 4 && path[0] == servicesKey && path[2] == "initContainer":
		return nil
	case len(path) == 3 && path[0] == "resources":
		return fmt.Errorf("line %d: resource %s overrides its image, which isn't allowed", line, path[1])
	}
	return fmt.Errorf("line %d: %s is an image the gateway doesn't check, so it isn't allowed",
		line, strings.Join(path, "."))
}
