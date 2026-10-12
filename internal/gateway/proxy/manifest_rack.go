package proxy

import (
	"fmt"
	"strings"

	convoxmanifest "github.com/convox/convox/pkg/manifest"
	"github.com/convox/convox/pkg/structs"
)

// loadManifestAsRack parses a manifest the way the rack does, with Convox's own manifest.Load: it substitutes each
// ${NAME} with env's value, then decodes the result with yaml.v2. parseManifest checks the text as written, but a
// value can change what the text means once it's substituted ("[" or "|" swallows the lines after it, a quote ends
// a quoted value early), and env is set apart from the manifest, by anyone allowed to set it. So the images the rack
// will use are checked in this parse too, with the env the rack will use.
func loadManifestAsRack(data []byte, env map[string]string) (*convoxManifest, error) {
	m, err := convoxmanifest.Load(data, env)
	if err != nil {
		return nil, fmt.Errorf("failed to load the manifest as the rack does: %w", err)
	}
	parsed := &convoxManifest{
		Services:  make(map[string]convoxService, len(m.Services)),
		Resources: make(map[string]convoxResource, len(m.Resources)),
	}
	for _, s := range m.Services {
		if _, ok := parsed.Services[s.Name]; ok {
			return nil, fmt.Errorf("service %s is defined more than once", s.Name)
		}
		service := convoxService{Image: s.Image}
		if s.InitContainer != nil {
			service.InitContainer = &convoxInitContainer{Image: s.InitContainer.Image}
		}
		parsed.Services[s.Name] = service
	}
	for _, r := range m.Resources {
		if _, ok := parsed.Resources[r.Name]; ok {
			return nil, fmt.Errorf("resource %s is defined more than once", r.Name)
		}
		parsed.Resources[r.Name] = convoxResource{Image: r.Image}
	}
	return parsed, nil
}

// parseRackEnv parses a release's env the way the rack does (structs.Environment).
func parseRackEnv(env string) (map[string]string, error) {
	parsed, err := structs.NewEnvironment([]byte(env))
	if err != nil {
		return nil, fmt.Errorf("failed to parse the release env: %w", err)
	}
	return parsed, nil
}

// buildManifestEnv is the env a build substitutes into its manifest: the app's latest release env, with the build's
// arguments over it (Convox pkg/build buildkit.go). checkBuildArgs has already refused any argument that isn't
// NAME=value.
func buildManifestEnv(appEnv map[string]string, buildArgs string) map[string]string {
	env := make(map[string]string, len(appEnv))
	for name, value := range appEnv {
		env[name] = value
	}
	if buildArgs == "" {
		return env
	}
	for _, arg := range strings.Split(buildArgs, ",") {
		if name, value, ok := strings.Cut(arg, "="); ok {
			env[name] = value
		}
	}
	return env
}
