package update

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

const (
	// githubActionsIssuer issues the OIDC tokens GitHub Actions jobs sign attestations with.
	githubActionsIssuer = "https://token.actions.githubusercontent.com"
	// slsaProvenanceV1 is the predicate type of the attestations actions/attest-build-provenance creates.
	slsaProvenanceV1 = "https://slsa.dev/provenance/v1"
)

// Release workflows sign with the public Sigstore instance's certificates, which are only valid for a few minutes,
// so a bundle must carry a transparency log entry and a signed certificate timestamp, as gh attestation verify
// requires.
var publicGoodVerifierOptions = []verify.VerifierOption{
	verify.WithSignedCertificateTimestamps(1),
	verify.WithTransparencyLog(1),
	verify.WithObserverTimestamps(1),
}

// SignerIdentity is the certificate identity of the Release workflow run that built the release with this tag.
func SignerIdentity(repository, tag string) string {
	return fmt.Sprintf("https://github.com/%s/.github/workflows/release.yml@refs/tags/%s", repository, tag)
}

// ProvenanceVerifier checks a release archive's build provenance attestation.
type ProvenanceVerifier struct {
	verifier   *verify.Verifier
	repository string
}

// NewProvenanceVerifier fetches the public Sigstore instance's trusted root through TUF (the root of trust is built
// into sigstore-go) and returns a verifier for this repository's releases.
func NewProvenanceVerifier(repository string) (*ProvenanceVerifier, error) {
	opts := tuf.DefaultOptions().WithDisableLocalCache()
	trustedRoot, err := root.FetchTrustedRootWithOptions(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch the Sigstore trusted root: %w", err)
	}
	return provenanceVerifierWith(trustedRoot, repository, publicGoodVerifierOptions...)
}

func provenanceVerifierWith(
	material root.TrustedMaterial,
	repository string,
	options ...verify.VerifierOption,
) (*ProvenanceVerifier, error) {
	verifier, err := verify.NewVerifier(material, options...)
	if err != nil {
		return nil, err
	}
	return &ProvenanceVerifier{verifier: verifier, repository: repository}, nil
}

// Verify checks that bundleJSON is a SLSA provenance attestation for an artifact with this SHA-256 digest, signed by
// the repository's Release workflow running for this tag.
func (p *ProvenanceVerifier) Verify(bundleJSON, digest []byte, tag string) error {
	var b bundle.Bundle
	if err := b.UnmarshalJSON(bundleJSON); err != nil {
		return fmt.Errorf("invalid attestation bundle: %w", err)
	}
	return p.verifyEntity(&b, digest, tag)
}

func (p *ProvenanceVerifier) verifyEntity(entity verify.SignedEntity, digest []byte, tag string) error {
	identity, err := verify.NewShortCertificateIdentity(githubActionsIssuer, "", SignerIdentity(p.repository, tag), "")
	if err != nil {
		return err
	}
	policy := verify.NewPolicy(verify.WithArtifactDigest("sha256", digest), verify.WithCertificateIdentity(identity))
	result, err := p.verifier.Verify(entity, policy)
	if err != nil {
		return fmt.Errorf("attestation verification failed: %w", err)
	}
	if result.Statement == nil || result.Statement.GetPredicateType() != slsaProvenanceV1 {
		return errors.New("attestation verification failed: not a SLSA build provenance attestation")
	}
	return nil
}

// VerifyChecksum checks data against a checksum file in sha256sum format ("<hex digest>  <name>"), which must have
// a line for name. It returns the data's SHA-256 digest.
func VerifyChecksum(data, checksums []byte, name string) ([]byte, error) {
	sum := sha256.Sum256(data)
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		expected, err := hex.DecodeString(fields[0])
		if err != nil {
			return nil, fmt.Errorf("invalid checksum for %s", name)
		}
		if !bytes.Equal(expected, sum[:]) {
			return nil, fmt.Errorf("checksum mismatch for %s", name)
		}
		return sum[:], nil
	}
	return nil, fmt.Errorf("no checksum for %s", name)
}
