//go:build !nofido

package webauthn

import (
	"errors"
	"testing"

	"github.com/keys-pub/go-libfido2"
	"github.com/stretchr/testify/require"
)

// fakePINKey is a pinKey with canned answers.
type fakePINKey struct {
	options  []libfido2.Option
	infoErr  error
	checkErr error
	checked  []string
	tries    int
	triesErr error
}

func (k *fakePINKey) Info() (*libfido2.DeviceInfo, error) {
	if k.infoErr != nil {
		return nil, k.infoErr
	}
	return &libfido2.DeviceInfo{Options: k.options}, nil
}

func (k *fakePINKey) CredentialsInfo(pin string) (*libfido2.CredentialsInfo, error) {
	k.checked = append(k.checked, pin)
	if k.checkErr != nil {
		return nil, k.checkErr
	}
	return &libfido2.CredentialsInfo{}, nil
}

func (k *fakePINKey) RetryCount() (int, error) {
	return k.tries, k.triesErr
}

// The PIN can be asked for ahead only when one key is connected and it has a PIN: with several, the challenge's
// credentials say which one signs.
func TestOnlyKeyHasPIN(t *testing.T) {
	withPIN := &fakePINKey{options: pinOptions}
	require.True(t, onlyKeyHasPIN([]pinKey{withPIN}))
	require.False(t, onlyKeyHasPIN([]pinKey{withPIN, &fakePINKey{options: pinOptions}}))
	require.False(t, onlyKeyHasPIN(nil))
	require.False(t, onlyKeyHasPIN([]pinKey{&fakePINKey{}}))
	require.False(t, onlyKeyHasPIN([]pinKey{&fakePINKey{options: pinOptions, infoErr: libfido2.ErrTX}}))
}

// A wrong PIN says how many tries are left; a PIN too short to be one isn't sent to the key. A key that can't check
// a PIN this way leaves it to the assertion, and a locked one ends the review.
func TestVerifyPIN(t *testing.T) {
	key := &fakePINKey{}
	require.NoError(t, verifyPIN(key, "123456"))
	require.Equal(t, []string{"123456"}, key.checked)

	key = &fakePINKey{checkErr: libfido2.ErrPinInvalid, tries: 6}
	err := verifyPIN(key, "123457")
	require.ErrorIs(t, err, ErrWrongPIN)
	require.EqualError(t, err, "wrong security key PIN: 6 tries left before the key locks")

	key.triesErr = errors.New("no answer")
	require.Equal(t, ErrWrongPIN, verifyPIN(key, "123457"))

	key = &fakePINKey{}
	err = verifyPIN(key, "123")
	require.ErrorIs(t, err, ErrWrongPIN)
	require.ErrorContains(t, err, "a PIN has at least 4 characters")
	require.Empty(t, key.checked)

	for _, unsupported := range []error{libfido2.ErrInvalidCommand, libfido2.ErrUnsupportedOption, libfido2.ErrTX} {
		require.NoError(t, verifyPIN(&fakePINKey{checkErr: unsupported}, "123456"))
	}

	err = verifyPIN(&fakePINKey{checkErr: libfido2.ErrPinAuthBlocked}, "123456")
	require.ErrorIs(t, err, libfido2.ErrPinAuthBlocked)
	require.NotErrorIs(t, err, ErrWrongPIN)
	require.ErrorContains(t, err, "unplug the security key")
}
