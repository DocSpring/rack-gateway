package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
)

// callWebLoginCallback runs the web login callback for target, with the OAuth state cookie the login started with,
// and returns the query of the login error page it redirects to.
func callWebLoginCallback(t *testing.T, oauth *fakeOAuth, target string) string {
	t.Helper()
	handler := NewAuthHandler(oauth, nil, &config.Config{DevMode: true}, nil, nil, nil, nil, nil)
	c, w := newTestContext(http.MethodGet, target)
	c.Request.AddCookie(testCookie(webOAuthStateCookie, "state"))

	handler.WebLoginCallback(c)

	require.Equal(t, http.StatusFound, w.Code)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, WebLoginErrorRoute, loc.Path)
	return loc.RawQuery
}

// The login error page is on the gateway's own domain, so the callback passes it only a reason code: never the
// identity provider's error description, or anything else from the request, that a crafted link could fill with
// its own text.
func TestWebLoginCallbackPassesOnlyAReasonCode(t *testing.T) {
	spoof := "Call+555-0100+to+restore+access"
	tests := []struct {
		target string
		want   string
	}{
		{"/api/v1/auth/web/callback?error=access_denied&error_description=" + spoof, "error=access_denied"},
		{"/api/v1/auth/web/callback?error=server_error&error_description=" + spoof, "error=identity_provider_error"},
		{"/api/v1/auth/web/callback?error=" + spoof, "error=identity_provider_error"},
		{"/api/v1/auth/web/callback?code=abc", "error=missing_code"},
		{"/api/v1/auth/web/callback?state=state", "error=missing_code"},
		{"/api/v1/auth/web/callback?code=abc&state=" + spoof, "error=state_mismatch"},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, callWebLoginCallback(t, &fakeOAuth{}, tt.target), tt.target)
	}
}

func TestWebLoginCallbackExchangeFailuresUseReasonCodes(t *testing.T) {
	target := "/api/v1/auth/web/callback?code=abc&state=state"

	denied := &fakeOAuth{completeErr: &auth.DomainNotAllowedError{Email: "user@other.example", Name: "User"}}
	require.Equal(t, "error=unauthorized", callWebLoginCallback(t, denied, target))

	failed := &fakeOAuth{completeErr: errors.New("token exchange failed")}
	require.Equal(t, "error=exchange_failed", callWebLoginCallback(t, failed, target))
}

func TestWebLoginCallbackWithoutSessionsUsesReasonCode(t *testing.T) {
	require.Equal(t, "error=session_failed",
		callWebLoginCallback(t, &fakeOAuth{}, "/api/v1/auth/web/callback?code=abc&state=state"))
}
