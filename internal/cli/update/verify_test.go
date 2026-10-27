package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"testing"

	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/testing/ca"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/stretchr/testify/require"
)

const (
	testRepository = "DocSpring/rack-gateway"
	testTag        = "v0.1.29"
	// The SHA-256 digest of the v0.1.28 rack-gateway-linux-amd64.tar.gz release archive.
	v0128LinuxAMD64Digest = "13679553cf7e1b4533f6b2f151b521cdd0b35b70c81488c683b32af41270d91e"
)

// The v0.1.28 release's real attestation, checked with the public Sigstore instance's trusted root as TUF served
// it in October 2026: the verifier options and policy accept what actions/attest-build-provenance creates.
func TestProvenanceVerifierAcceptsARealReleaseAttestation(t *testing.T) {
	bundleJSON, err := os.ReadFile("testdata/rack-gateway-linux-amd64-v0.1.28.sigstore.json")
	require.NoError(t, err)
	trustedRoot, err := root.NewTrustedRootFromPath("testdata/public-good-trusted-root.json")
	require.NoError(t, err)
	verifier, err := provenanceVerifierWith(trustedRoot, testRepository, publicGoodVerifierOptions...)
	require.NoError(t, err)
	digest, err := hex.DecodeString(v0128LinuxAMD64Digest)
	require.NoError(t, err)

	require.NoError(t, verifier.Verify(bundleJSON, digest, "v0.1.28"))

	require.ErrorContains(t, verifier.Verify(bundleJSON, digest, "v0.1.27"), "attestation verification failed")
	otherDigest := sha256.Sum256([]byte("another archive"))
	require.ErrorContains(t, verifier.Verify(bundleJSON, otherDigest[:], "v0.1.28"), "attestation verification failed")
	fork, err := provenanceVerifierWith(trustedRoot, "someone/rack-gateway", publicGoodVerifierOptions...)
	require.NoError(t, err)
	require.ErrorContains(t, fork.Verify(bundleJSON, digest, "v0.1.28"), "attestation verification failed")
	require.ErrorContains(t, verifier.Verify([]byte(`{}`), digest, "v0.1.28"), "invalid attestation bundle")
}

func TestProvenanceVerifierChecksSignerDigestAndPredicate(t *testing.T) {
	sigstore, err := ca.NewVirtualSigstore()
	require.NoError(t, err)
	verifier, err := provenanceVerifierWith(sigstore, testRepository,
		verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1))
	require.NoError(t, err)
	digest := sha256.Sum256([]byte("release archive"))

	tests := []struct {
		name      string
		identity  string
		issuer    string
		predicate string
		wantErr   string
	}{
		{
			name:      "the release workflow for this tag",
			identity:  SignerIdentity(testRepository, testTag),
			issuer:    githubActionsIssuer,
			predicate: slsaProvenanceV1,
		},
		{
			name:      "the release workflow for another tag",
			identity:  SignerIdentity(testRepository, "v0.1.28"),
			issuer:    githubActionsIssuer,
			predicate: slsaProvenanceV1,
			wantErr:   "attestation verification failed",
		},
		{
			name:      "the release workflow from a branch",
			identity:  "https://github.com/DocSpring/rack-gateway/.github/workflows/release.yml@refs/heads/main",
			issuer:    githubActionsIssuer,
			predicate: slsaProvenanceV1,
			wantErr:   "attestation verification failed",
		},
		{
			name:      "another workflow",
			identity:  "https://github.com/DocSpring/rack-gateway/.github/workflows/ci.yml@refs/tags/" + testTag,
			issuer:    githubActionsIssuer,
			predicate: slsaProvenanceV1,
			wantErr:   "attestation verification failed",
		},
		{
			name:      "another repository",
			identity:  SignerIdentity("someone/rack-gateway", testTag),
			issuer:    githubActionsIssuer,
			predicate: slsaProvenanceV1,
			wantErr:   "attestation verification failed",
		},
		{
			name:      "another OIDC issuer",
			identity:  SignerIdentity(testRepository, testTag),
			issuer:    "https://accounts.google.com",
			predicate: slsaProvenanceV1,
			wantErr:   "attestation verification failed",
		},
		{
			name:      "not build provenance",
			identity:  SignerIdentity(testRepository, testTag),
			issuer:    githubActionsIssuer,
			predicate: "https://in-toto.io/attestation/release/v0.1",
			wantErr:   "not a SLSA build provenance attestation",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entity, err := sigstore.Attest(tt.identity, tt.issuer, statement(tt.predicate, digest[:]))
			require.NoError(t, err)
			err = verifier.verifyEntity(entity, digest[:], testTag)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}

	t.Run("another artifact", func(t *testing.T) {
		entity, err := sigstore.Attest(SignerIdentity(testRepository, testTag), githubActionsIssuer,
			statement(slsaProvenanceV1, digest[:]))
		require.NoError(t, err)
		other := sha256.Sum256([]byte("another archive"))
		require.ErrorContains(t, verifier.verifyEntity(entity, other[:], testTag), "attestation verification failed")
	})
}

func statement(predicateType string, digest []byte) []byte {
	return fmt.Appendf(nil,
		`{"_type":"https://in-toto.io/Statement/v1","predicateType":%q,`+
			`"subject":[{"name":"rack-gateway-linux-amd64.tar.gz","digest":{"sha256":%q}}],"predicate":{}}`,
		predicateType, hex.EncodeToString(digest))
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("release archive")
	sum := sha256.Sum256(data)
	hexSum := hex.EncodeToString(sum[:])

	digest, err := VerifyChecksum(data, []byte(hexSum+"  a.tar.gz\n"), "a.tar.gz")
	require.NoError(t, err)
	require.Equal(t, sum[:], digest)

	// sha256sum's binary mode marks the name with "*".
	_, err = VerifyChecksum(data, []byte(hexSum+" *a.tar.gz\n"), "a.tar.gz")
	require.NoError(t, err)

	other := sha256.Sum256([]byte("tampered"))
	_, err = VerifyChecksum(data, []byte(hex.EncodeToString(other[:])+"  a.tar.gz\n"), "a.tar.gz")
	require.ErrorContains(t, err, "checksum mismatch for a.tar.gz")

	_, err = VerifyChecksum(data, []byte(hexSum+"  b.tar.gz\n"), "a.tar.gz")
	require.ErrorContains(t, err, "no checksum for a.tar.gz")

	_, err = VerifyChecksum(data, []byte("not-hex  a.tar.gz\n"), "a.tar.gz")
	require.ErrorContains(t, err, "invalid checksum for a.tar.gz")
}
