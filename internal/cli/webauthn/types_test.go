package webauthn

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTouchReason(t *testing.T) {
	options := AssertionOptions{Origin: "https://gateway-us.example.com"}
	require.Equal(t, "authenticate to gateway-us.example.com", options.TouchReason())

	options.Prompt.Touch = "approve api on staging (2 of 4)"
	require.Equal(t, "approve api on staging (2 of 4)", options.TouchReason())

	require.Equal(t, "authenticate", AssertionOptions{Origin: "not a url"}.TouchReason())
}
