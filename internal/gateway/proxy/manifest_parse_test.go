package proxy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const goodImage = "docker.io/org/app:abc"

// checkManifest parses a manifest and applies a "*" pattern that only allows goodImage, as a build of an app with
// service_image_patterns would.
func checkManifest(manifest string) error {
	parsed, err := parseManifest([]byte(manifest))
	if err != nil {
		return err
	}
	return newImagePolicy(map[string]string{"*": `docker\.io/org/app:abc`}, "", false).validate(parsed)
}

// The rack replaces every match of \$\{([^}]*?)\} in the raw manifest before it parses it, and a match can span
// lines and quotes. Here the gateway's parse sees web's pinned image, but the rack's substitution swallows
// everything from "${X" to the "}" two lines down, image included: the rack sees `description: ""` and builds web
// from the archive's source.
func TestManifestRefusesInterpolationSpanningLines(t *testing.T) {
	manifest := "services:\n" +
		"  web:\n" +
		"    description: \"${X\"\n" +
		"    image: " + goodImage + "\n" +
		"    x: a}\"\n"

	err := checkManifest(manifest)
	require.ErrorContains(t, err, "manifest line 3 uses ${...} other than ${NAME}")
	require.ErrorContains(t, err, "the rack substitutes ${...} before it parses the manifest")
}

// A well-formed ${NAME} can still change the manifest's structure when it's somewhere other than a value: unset,
// it vanishes, and what's left is parsed as YAML.
func TestManifestRefusesInterpolationThatCanChangeStructure(t *testing.T) {
	cases := map[string]string{
		// "${X}image" is an unknown key to the gateway; with X unset the rack sees a second image key, and its
		// YAML parser keeps the last one.
		"in a key": "services:\n  web:\n    image: " + goodImage + "\n    ${X}image: attacker/app\n",
		// With X unset, the rack parses this init container as a mapping with an image.
		"before a flow mapping": "services:\n  web:\n    image: " + goodImage +
			"\n    initContainer: ${X}{\"image\":\"attacker/app\"}\n",
		// With X unset, "- b" becomes a sequence entry.
		"before an indicator": "services:\n  web:\n    image: " + goodImage + "\n    command:\n      ${X}- b\n",
		"in a comment":        "services:\n  web:\n    image: " + goodImage + " # ${X}\n",
		"in an unquoted value with spaces": "services:\n  web:\n    image: " + goodImage +
			"\n    command: bin/run ${X} --flag\n",
	}
	for name, manifest := range cases {
		t.Run(name, func(t *testing.T) {
			require.ErrorContains(t, checkManifest(manifest), "the rack substitutes ${...}")
		})
	}
}

func TestManifestAcceptsInterpolatedValues(t *testing.T) {
	manifest := "services:\n" +
		"  web:\n" +
		"    image: " + goodImage + "\n" +
		"    domain: ${HOST}\n" +
		"    command: \"bin/start --mode ${MODE} # not a comment\"\n" +
		"    environment:\n" +
		"      - PORT=${PORT}\n" +
		"      - 'GREETING=hello ${NAME}'\n" +
		"    scale:\n" +
		"      count: ${COUNT} # replicas\n" +
		"    annotations:\n" +
		"      - tailscale.com/hostname: app-web-${RACK}\n"
	require.NoError(t, checkManifest(manifest))
}

// A ${NAME} in an image is substituted after the check, so the image the rack runs isn't the one checked.
func TestManifestRefusesInterpolatedImage(t *testing.T) {
	manifest := "services:\n  web:\n    image: \"docker.io/org/app:${TAG}\"\n"
	require.ErrorContains(t, checkManifest(manifest), "uses ${...}, which the rack substitutes after the check")

	permissive := map[string]string{"*": `docker\.io/org/app:.*`}
	parsed, err := parseManifest([]byte(manifest))
	require.NoError(t, err)
	require.ErrorContains(t, newImagePolicy(permissive, "", false).validate(parsed), "uses ${...}")
}

// An init container runs with its service's environment, so its image is held to the service's pattern. Without an
// image of its own, it runs the service's image.
func TestManifestChecksInitContainerImages(t *testing.T) {
	withInit := func(init string) string {
		return "services:\n  web:\n    image: " + goodImage + "\n    initContainer:\n" + init
	}

	err := checkManifest(withInit("      image: attacker/app\n"))
	require.ErrorContains(t, err, `service web init container image "attacker/app" does not match required pattern`)

	require.NoError(t, checkManifest(withInit("      image: "+goodImage+"\n")))
	require.NoError(t, checkManifest(withInit("      command: bin/migrate\n")))
}

func TestManifestRefusesImagesTheGatewayDoesNotCheck(t *testing.T) {
	cases := map[string]struct{ manifest, want string }{
		"resource image override": {
			manifest: "resources:\n  db:\n    type: postgres\n    image: attacker/postgres\n" +
				"services:\n  web:\n    image: " + goodImage + "\n",
			want: "resource db overrides its image",
		},
		"timer image": {
			manifest: "services:\n  web:\n    image: " + goodImage + "\n" +
				"timers:\n  cleanup:\n    schedule: \"0 3 * * *\"\n    service: web\n    image: attacker/app\n",
			want: "timers.cleanup.image is an image the gateway doesn't check",
		},
		"image nested in a service option": {
			manifest: "services:\n  web:\n    image: " + goodImage + "\n" +
				"    volumeOptions:\n      - image: attacker/app\n",
			want: "services.web.volumeOptions.-.image is an image the gateway doesn't check",
		},
		"image under an unknown top-level key": {
			manifest: "x-extra:\n  image: attacker/app\nservices:\n  web:\n    image: " + goodImage + "\n",
			want:     "x-extra.image is an image the gateway doesn't check",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.ErrorContains(t, checkManifest(tc.manifest), tc.want)
		})
	}
}

// The rack parses manifests with a different YAML parser. Merge keys, anchors that carry images and repeated keys
// are where the two can disagree on a service's image, so they're refused.
func TestManifestRefusesAmbiguousYAML(t *testing.T) {
	cases := map[string]struct{ manifest, want string }{
		// The rack's parser applies the merge after the explicit services key and adds a second, source-built web.
		"merge key": {
			manifest: "x-base: &base\n  services:\n    web:\n      build: .\n" +
				"services:\n  web:\n    image: " + goodImage + "\n<<: *base\n",
			want: "YAML merge keys (<<) aren't allowed",
		},
		"anchored init container": {
			manifest: "services:\n" +
				"  web:\n    image: " + goodImage + "\n    initContainer: &init\n      image: " + goodImage + "\n" +
				"  worker:\n    image: " + goodImage + "\n    initContainer: *init\n",
			want: "anchor &init holds an image key",
		},
		"anchored service": {
			manifest: "services:\n  web: &web\n    image: " + goodImage + "\n  worker: *web\n",
			want:     "anchor &web holds an image key",
		},
		// !!binary aW1hZ2U= decodes to "image": the same key twice, spelled two ways.
		"key repeated in another spelling": {
			manifest: "services:\n  web:\n    image: " + goodImage + "\n    !!binary aW1hZ2U=: attacker/app\n",
			want:     `key "image" appears more than once`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.ErrorContains(t, checkManifest(tc.manifest), tc.want)
		})
	}
}

// Anchors and aliases that carry no image are fine.
func TestManifestAcceptsAnchorsWithoutImages(t *testing.T) {
	manifest := "x-env: &env\n  - PORT=3000\n" +
		"services:\n" +
		"  web:\n    image: " + goodImage + "\n    environment: *env\n" +
		"  worker:\n    image: " + goodImage + "\n    environment: *env\n"
	require.NoError(t, checkManifest(manifest))
}

func TestObjectKeyForApp(t *testing.T) {
	key, err := objectKeyForApp("object://myapp/tmp/source.tgz", "myapp")
	require.NoError(t, err)
	require.Equal(t, "tmp/source.tgz", key)

	// The rack builds the object named by the URL: another app's, or a different key once the URL is parsed.
	refused := []string{
		"object://otherapp/tmp/source.tgz",
		"object://myapp:80/tmp/source.tgz",
		"object://user@myapp/tmp/source.tgz",
		"object://myapp/tmp/source.tgz?x=1",
		"object://myapp/tmp/source.tgz#x",
		"object://myapp/tmp/source%2Etgz",
		"object://myapp/",
		"OBJECT://myapp/tmp/source.tgz",
		"https://myapp/tmp/source.tgz",
	}
	for _, objectURL := range refused {
		_, err := objectKeyForApp(objectURL, "myapp")
		require.Errorf(t, err, "object URL %q", objectURL)
	}
}
