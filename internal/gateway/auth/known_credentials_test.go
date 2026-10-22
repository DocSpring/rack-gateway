package auth

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A credential is known for knownCredentialTTL after it last authenticated. The set holds at most max credentials:
// a full set makes room by dropping expired ones, and otherwise doesn't add a new one.
func TestKnownCredentialsExpireAndStayBounded(t *testing.T) {
	known := newKnownCredentials()
	clock := time.Now()
	known.now = func() time.Time { return clock }
	known.max = 2

	require.False(t, known.has("a"))
	known.remember("a")
	require.True(t, known.has("a"))
	known.remember("")
	require.False(t, known.has(""), "no credential is never known")

	clock = clock.Add(knownCredentialTTL - time.Second)
	known.remember("b")
	require.True(t, known.has("a"))
	known.remember("c")
	require.False(t, known.has("c"), "the set is full")

	clock = clock.Add(time.Second)
	require.False(t, known.has("a"), "a expired")
	known.remember("c")
	require.True(t, known.has("c"), "a full set drops expired credentials to make room")
	require.True(t, known.has("b"))

	known.forget("b")
	require.False(t, known.has("b"))

	var none *knownCredentials
	none.remember("a")
	require.False(t, none.has("a"))
}

// The credential is the session or API token authenticate reads, without inline MFA: the Authorization header's,
// or else the session cookie's.
func TestPresentedCredential(t *testing.T) {
	basic := func(userPass string) string {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(userPass))
	}
	for _, tc := range []struct {
		authorization, cookie, want string
	}{
		{"Bearer rgw_abc", "", "rgw_abc"},
		{"Bearer session123", "", "session123"},
		{"Bearer session123.totp.123456", "", "session123"},
		{basic("convox:session123.totp.123456"), "", "session123"},
		{basic("convox:rgw_abc"), "", "rgw_abc"},
		{"Basic !!!", "", ""},
		{"Digest x", "", ""},
		{"Bearer rgw_abc", "cookie-session", "rgw_abc"},
		{"", "cookie-session", "cookie-session"},
		{"", "", ""},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if tc.authorization != "" {
			r.Header.Set("Authorization", tc.authorization)
		}
		if tc.cookie != "" {
			r.AddCookie(&http.Cookie{
				Name: "session_token", Value: tc.cookie,
				Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
			})
		}
		require.Equal(t, tc.want, presentedCredential(r), "%q %q", tc.authorization, tc.cookie)
	}
}
