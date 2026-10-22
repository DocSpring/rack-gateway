//go:build !nofido

package webauthn

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/keys-pub/go-libfido2"
	"github.com/stretchr/testify/require"
)

var (
	credentialA = []byte("credential-a")
	credentialB = []byte("credential-b")
	pinOptions  = []libfido2.Option{{Name: "clientPin", Value: libfido2.True}}
)

// fakeKey is a securityKey that gives canned answers and records what it was asked.
type fakeKey struct {
	// infoErrs are the errors of successive Info calls. A nil entry, or a call past the end, succeeds.
	infoErrs  []error
	options   []libfido2.Option
	infoCalls int

	credentials      []*libfido2.Credential
	credentialsErr   error
	credentialsPIN   string
	credentialsCalls int

	assertionErr       error
	assertionCalls     int
	assertionPIN       string
	assertionAllowList [][]byte
	assertionOpts      *libfido2.AssertionOpts
}

func (k *fakeKey) Info() (*libfido2.DeviceInfo, error) {
	k.infoCalls++
	if k.infoCalls <= len(k.infoErrs) && k.infoErrs[k.infoCalls-1] != nil {
		return nil, k.infoErrs[k.infoCalls-1]
	}
	return &libfido2.DeviceInfo{Options: k.options}, nil
}

func (k *fakeKey) Credentials(_ string, pin string) ([]*libfido2.Credential, error) {
	k.credentialsCalls++
	k.credentialsPIN = pin
	return k.credentials, k.credentialsErr
}

func (k *fakeKey) Assertion(
	_ string, _ []byte, credentialIDs [][]byte, pin string, opts *libfido2.AssertionOpts,
) (*libfido2.Assertion, error) {
	k.assertionCalls++
	k.assertionPIN = pin
	k.assertionAllowList = credentialIDs
	k.assertionOpts = opts
	if k.assertionErr != nil {
		return nil, k.assertionErr
	}
	authData, err := cbor.Marshal([]byte("authenticator-data"))
	if err != nil {
		return nil, err
	}
	return &libfido2.Assertion{AuthDataCBOR: authData, Sig: []byte("signature"), CredentialID: credentialA}, nil
}

func testAssertionOptions() AssertionOptions {
	return AssertionOptions{
		Challenge: "challenge",
		RPID:      "gateway.example.com",
		AllowCredentials: []string{
			base64.RawURLEncoding.EncodeToString(credentialA),
			base64.RawURLEncoding.EncodeToString(credentialB),
		},
		UserVerification: "required",
		Origin:           "https://gateway.example.com",
	}
}

// testFlow returns a flow on key whose PIN prompt answers pin and counts in prompts how often it was shown.
func testFlow(key *fakeKey, pin string, prompts *int) assertionFlow {
	return assertionFlow{
		key: key,
		readPIN: func() (string, error) {
			*prompts++
			return pin, nil
		},
	}
}

func TestGetAssertionFailsWhenKeyInfoCantBeRead(t *testing.T) {
	readErr := fmt.Errorf("failed to get info: %w", libfido2.ErrRX)
	key := &fakeKey{infoErrs: []error{readErr, readErr}, options: pinOptions}
	prompts := 0

	_, _, err := testFlow(key, "1234", &prompts).getAssertion(testAssertionOptions(), "")

	require.ErrorIs(t, err, libfido2.ErrRX)
	require.ErrorContains(t, err, "couldn't read the security key (failed to get info: rx error)")
	require.ErrorContains(t, err, "make sure no other app is using it")
	require.Equal(t, 2, key.infoCalls)
	require.Zero(t, prompts)
	require.Zero(t, key.credentialsCalls)
	require.Zero(t, key.assertionCalls)
}

func TestGetAssertionRetriesKeyInfoOnce(t *testing.T) {
	key := &fakeKey{infoErrs: []error{libfido2.ErrNotFIDO2}, options: pinOptions}
	prompts := 0

	resp, pin, err := testFlow(key, "1234", &prompts).getAssertion(testAssertionOptions(), "")

	require.NoError(t, err)
	require.Equal(t, 2, key.infoCalls)
	require.Equal(t, 1, prompts)
	require.Equal(t, 1, key.assertionCalls)
	require.Equal(t, "1234", key.assertionPIN)
	require.Equal(t, &libfido2.AssertionOpts{UV: libfido2.True}, key.assertionOpts)
	require.Equal(t, "1234", pin)
	require.Equal(t, base64.RawURLEncoding.EncodeToString(credentialA), resp.CredentialID)
}

func TestGetAssertionRefusesUserVerificationWithoutPIN(t *testing.T) {
	key := &fakeKey{options: pinOptions}
	prompts := 0

	_, _, err := testFlow(key, "", &prompts).getAssertion(testAssertionOptions(), "")

	require.ErrorIs(t, err, errNoPINEntered)
	require.Equal(t, 1, prompts)
	require.Zero(t, key.credentialsCalls)
	require.Zero(t, key.assertionCalls)
}

func TestGetAssertionAllowsEmptyPINForKeyWithBuiltInUserVerification(t *testing.T) {
	key := &fakeKey{options: append([]libfido2.Option{{Name: "uv", Value: libfido2.True}}, pinOptions...)}
	prompts := 0

	_, pin, err := testFlow(key, "", &prompts).getAssertion(testAssertionOptions(), "")

	require.NoError(t, err)
	require.Empty(t, pin)
	require.Equal(t, 1, key.assertionCalls)
	require.Equal(t, &libfido2.AssertionOpts{UV: libfido2.True}, key.assertionOpts)
}

func TestGetAssertionWithoutUserVerificationAllowsEmptyPIN(t *testing.T) {
	key := &fakeKey{options: pinOptions}
	prompts := 0
	options := testAssertionOptions()
	options.UserVerification = "discouraged"

	_, pin, err := testFlow(key, "", &prompts).getAssertion(options, "")

	require.NoError(t, err)
	require.Empty(t, pin)
	require.Equal(t, 1, key.assertionCalls)
	require.Nil(t, key.assertionOpts)
}

func TestGetAssertionSkipsPINForKeyWithoutOne(t *testing.T) {
	key := &fakeKey{}
	prompts := 0

	_, pin, err := testFlow(key, "1234", &prompts).getAssertion(testAssertionOptions(), "")

	require.NoError(t, err)
	require.Empty(t, pin)
	require.Zero(t, prompts)
	require.Zero(t, key.credentialsCalls)
	require.Equal(t, [][]byte{credentialA, credentialB}, key.assertionAllowList)
	require.Empty(t, key.assertionPIN)
}

func TestGetAssertionWithCachedPINSkipsInfoAndPrompt(t *testing.T) {
	key := &fakeKey{options: pinOptions}
	prompts := 0

	_, pin, err := testFlow(key, "1234", &prompts).getAssertion(testAssertionOptions(), "5678")

	require.NoError(t, err)
	require.Equal(t, "5678", pin)
	require.Zero(t, key.infoCalls)
	require.Zero(t, prompts)
	require.Equal(t, "5678", key.credentialsPIN)
	require.Equal(t, "5678", key.assertionPIN)
}

func TestGetAssertionNarrowsAllowListToKeyCredentials(t *testing.T) {
	key := &fakeKey{options: pinOptions, credentials: []*libfido2.Credential{{ID: credentialB}}}
	prompts := 0

	_, _, err := testFlow(key, "1234", &prompts).getAssertion(testAssertionOptions(), "")

	require.NoError(t, err)
	require.Equal(t, "1234", key.credentialsPIN)
	require.Equal(t, [][]byte{credentialB}, key.assertionAllowList)
}

func TestGetAssertionFailsWhenCredentialsCantBeListed(t *testing.T) {
	tests := []struct {
		listErr error
		want    string
	}{
		{libfido2.ErrPinInvalid, "wrong security key PIN"},
		{libfido2.ErrPinAuthBlocked, "unplug the security key, plug it back in"},
		{errPINBlocked, "the security key's PIN is blocked"},
		{libfido2.ErrOperationDenied, "failed to list the security key's credentials"},
	}
	for _, tt := range tests {
		t.Run(tt.listErr.Error(), func(t *testing.T) {
			key := &fakeKey{
				options:        pinOptions,
				credentialsErr: fmt.Errorf("failed to get resident key info: %w", tt.listErr),
			}
			prompts := 0

			_, _, err := testFlow(key, "1234", &prompts).getAssertion(testAssertionOptions(), "")

			require.ErrorIs(t, err, tt.listErr)
			require.ErrorContains(t, err, tt.want)
			require.Zero(t, key.assertionCalls)
		})
	}
}

func TestGetAssertionUsesWholeAllowListWhenKeyCantListCredentials(t *testing.T) {
	unsupported := []error{
		libfido2.ErrInvalidCommand,
		libfido2.ErrUnsupportedOption,
		errUnauthorizedPermission,
		libfido2.ErrNoCredentials,
		libfido2.ErrTX,
		libfido2.ErrRX,
		errChannelBusy,
	}
	for _, listErr := range unsupported {
		t.Run(listErr.Error(), func(t *testing.T) {
			key := &fakeKey{
				options:        pinOptions,
				credentialsErr: fmt.Errorf("failed to get resident key info: %w", listErr),
			}
			prompts := 0

			_, _, err := testFlow(key, "1234", &prompts).getAssertion(testAssertionOptions(), "")

			require.NoError(t, err)
			require.Equal(t, 1, key.credentialsCalls)
			require.Equal(t, [][]byte{credentialA, credentialB}, key.assertionAllowList)
		})
	}
}

func TestGetAssertionUsesWholeAllowListWhenKeyListsNoCredentials(t *testing.T) {
	key := &fakeKey{options: pinOptions, credentials: []*libfido2.Credential{}}
	prompts := 0

	_, _, err := testFlow(key, "1234", &prompts).getAssertion(testAssertionOptions(), "")

	require.NoError(t, err)
	require.Equal(t, [][]byte{credentialA, credentialB}, key.assertionAllowList)
}

// A key lists only its discoverable credentials: one holding a registered non-discoverable credential next to an
// unrelated discoverable one gets the whole allow list.
func TestGetAssertionUsesWholeAllowListWhenNoListedCredentialMatches(t *testing.T) {
	key := &fakeKey{options: pinOptions, credentials: []*libfido2.Credential{{ID: []byte("other-site")}}}
	prompts := 0

	_, _, err := testFlow(key, "1234", &prompts).getAssertion(testAssertionOptions(), "")

	require.NoError(t, err)
	require.Equal(t, [][]byte{credentialA, credentialB}, key.assertionAllowList)
}

// With several keys connected, the one holding a registered credential is used, found without a touch.
func TestChooseKeyPicksTheKeyHoldingACredential(t *testing.T) {
	allowList := [][]byte{credentialA}
	other := &fakeKey{assertionErr: libfido2.ErrNoCredentials}
	registered := &fakeKey{}

	require.Same(t, registered, chooseKey([]securityKey{other, registered}, "gateway.example.com", allowList))
	require.Equal(t, libfido2.False, other.assertionOpts.UP, "the probe needs no touch")
	require.Empty(t, registered.assertionPIN)

	busy := &fakeKey{assertionErr: errChannelBusy}
	unknown := &fakeKey{assertionErr: libfido2.ErrNoCredentials}
	require.Same(t, busy, chooseKey([]securityKey{busy, unknown}, "gateway.example.com", allowList),
		"the first key when none says it holds a credential")

	only := &fakeKey{}
	require.Same(t, only, chooseKey([]securityKey{only}, "gateway.example.com", allowList))
	require.Zero(t, only.assertionCalls, "a single key isn't probed")
}

func TestGetAssertionExplainsAssertionErrors(t *testing.T) {
	unsupported := fmt.Errorf("failed to get assertion: %w", libfido2.ErrUnsupportedOption)
	tests := []struct {
		name      string
		options   []libfido2.Option
		cachedPIN string
		err       error
		want      string
	}{
		{
			name: "unsupported option without a PIN",
			err:  unsupported,
			want: "the security key can't verify you without a PIN (failed to get assertion: unsupported option)",
		},
		{
			name:      "unsupported option with a PIN",
			options:   pinOptions,
			cachedPIN: "1234",
			err:       unsupported,
			want:      "couldn't read the security key (failed to get assertion: unsupported option)",
		},
		{
			name:      "wrong PIN",
			options:   pinOptions,
			cachedPIN: "1234",
			err:       fmt.Errorf("failed to get assertion: %w", libfido2.ErrPinInvalid),
			want:      "wrong security key PIN (failed to get assertion: pin invalid); try again",
		},
		{
			name:      "no registered credential",
			options:   pinOptions,
			cachedPIN: "1234",
			err:       fmt.Errorf("failed to get assertion: %w", libfido2.ErrNoCredentials),
			want:      "this security key holds none of your registered credentials for this gateway",
		},
		{
			name:      "other errors",
			options:   pinOptions,
			cachedPIN: "1234",
			err:       fmt.Errorf("failed to get assertion: %w", libfido2.ErrOperationDenied),
			want:      "failed to get assertion: failed to get assertion: operation denied",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := &fakeKey{options: tt.options, credentialsErr: libfido2.ErrNoCredentials, assertionErr: tt.err}
			prompts := 0

			_, _, err := testFlow(key, "", &prompts).getAssertion(testAssertionOptions(), tt.cachedPIN)

			require.ErrorIs(t, err, tt.err)
			require.ErrorContains(t, err, tt.want)
			require.Equal(t, 1, key.assertionCalls)
		})
	}
}
