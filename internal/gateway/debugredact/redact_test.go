package debugredact

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// redactedQueryValue is Redacted as a query string encodes it.
const redactedQueryValue = "%5BREDACTED%5D"

func TestSensitiveHeaders(t *testing.T) {
	for _, name := range []string{
		"Authorization", "Proxy-Authorization", "Cookie", "Set-Cookie", "X-Csrf-Token", "X-Mfa-Totp",
		"X-Mfa-Webauthn", "X-Mfa-Required", "X-Postmark-Server-Token", "X-Api-Key", "X-Client-Secret",
		"X-Admin-Password",
	} {
		require.Equal(t, Redacted, HeaderValue(name, "hidden-value"), name)
	}
	for _, name := range []string{"Accept", "Content-Type", "User-Agent", "X-Request-Id", "X-Forwarded-For"} {
		require.Equal(t, "visible-value", HeaderValue(name, "visible-value"), name)
	}
}

// A redirect can carry a login code and state (the CLI's loopback redirect does), and any URL can carry
// credentials.
func TestHeaderValueRedactsURLs(t *testing.T) {
	require.Equal(t,
		"http://127.0.0.1:54321/callback?code="+redactedQueryValue+"&state="+redactedQueryValue,
		HeaderValue("Location", "http://127.0.0.1:54321/callback?code=hidden-code&state=hidden-state"))
	require.Equal(t,
		"/app/auth/mfa/challenge?channel=cli&state="+redactedQueryValue,
		HeaderValue("Location", "/app/auth/mfa/challenge?state=hidden-state&channel=cli"))
	require.Equal(t,
		"https://gateway.example.com/app/rack?error=expired",
		HeaderValue("Referer", "https://user:hidden-password@gateway.example.com/app/rack?error=expired#hidden-token"))
	require.Equal(t,
		`<https://rack.example.com/apps?token=`+redactedQueryValue+`>; rel="next"`,
		HeaderValue("Link", `<https://rack.example.com/apps?token=hidden-token>; rel="next"`))
	require.Equal(t,
		"0; url=https://gateway.example.com/?login_code="+redactedQueryValue,
		HeaderValue("Refresh", "0; url=https://gateway.example.com/?login_code=hidden-code"))
}

// The URL pattern's [^\s...] is the whitespace class, so a URL runs on past an "s" in its host, user or path.
func TestURLsWithAnSAreRedactedWhole(t *testing.T) {
	require.Equal(t,
		`see <https://docs.example.com/sso?token=`+redactedQueryValue+`>`,
		HeaderValue("Link", `see <https://docs.example.com/sso?token=hidden-token>`))
	require.JSONEq(t,
		`{"note":"see https://docs.example.com/sso?token=`+redactedQueryValue+`"}`,
		Body([]byte(`{"note":"see https://docs.example.com/sso?token=hidden-token"}`), 1000))
	require.JSONEq(t,
		`{"note":"[REDACTED]"}`,
		Body([]byte(`{"note":"pull from https://sam:hidden-secret@registry.example.com/images"}`), 1000))
}

func TestHeadersAreSortedAndRedacted(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-Request-Id", "req-1")
	headers.Set("Authorization", "Bearer hidden-token")
	headers.Add("Set-Cookie", "a=hidden-1")
	headers.Add("Set-Cookie", "b=hidden-2")
	headers.Set("Location", "/callback?state=hidden-state")

	require.Equal(t,
		"Authorization: [REDACTED]\n"+
			"Location: /callback?state="+redactedQueryValue+"\n"+
			"Set-Cookie: [REDACTED]\n"+
			"Set-Cookie: [REDACTED]\n"+
			"X-Request-Id: req-1",
		Headers(headers))
	require.Empty(t, Headers(http.Header{}))
}

func TestURL(t *testing.T) {
	require.Equal(t,
		"https://gateway.example.com/api/v1/auth/cli/return?state="+redactedQueryValue,
		URL("https://convox:hidden-password@gateway.example.com/api/v1/auth/cli/return?state=hidden-state"))
	require.Equal(t, "https://rack.example.com/apps/web/releases", URL("https://rack.example.com/apps/web/releases"))
	require.Equal(t, Redacted, URL("https://rack.example.com/%zz"))
	require.Equal(t, "/apps?"+redactedQueryValue, URL("/apps?a=%zz&token=hidden-token"))
}

func TestBodyRedactsSensitiveFields(t *testing.T) {
	tests := []struct {
		body string
		want string
	}{
		{
			`{"login_code":"x","code_verifier":"x","backup_codes":["x"],"pin":1234}`,
			`{"backup_codes":"[REDACTED]","code_verifier":"[REDACTED]","login_code":"[REDACTED]","pin":"[REDACTED]"}`,
		},
		{
			`{"session_data":"x","assertion_response":{"id":"x"},"yubi_otp":"x","trusted_device_cookie":"x"}`,
			`{"assertion_response":"[REDACTED]","session_data":"[REDACTED]","trusted_device_cookie":"[REDACTED]",` +
				`"yubi_otp":"[REDACTED]"}`,
		},
		{`[{"secret":"x","id":12345678901234567890}]`, `[{"id":12345678901234567890,"secret":"[REDACTED]"}]`},
		{`{"env":["DATABASE_URL=x"]}`, `{"env":"[REDACTED]"}`},
		{`{"set":{}, "remove":[]}`, `{"remove":[],"set":{}}`},
		{`"just a string"`, `"just a string"`},
	}
	for _, tt := range tests {
		require.JSONEq(t, tt.want, Body([]byte(tt.body), 1000), tt.body)
	}
}

// A rack's resources list their URLs with credentials, and a release carries its env.
func TestBodyRedactsRackSecrets(t *testing.T) {
	body := `[{"name":"db","type":"postgres","url":"postgres://app:hidden-password@db.internal:5432/app"},` +
		`{"name":"cache","options":{"endpoint":"redis://:hidden-password@cache.internal:6379"},` +
		`"note":"see https://docs.example.com/x?token=hidden-token"},` +
		`{"id":"RABC","env":"DATABASE_URL=postgres://hidden\nRAILS_ENV=production",` +
		`"build_url":"https://ci.example.com/1"}]`

	logged := Body([]byte(body), 1000)

	require.NotContains(t, logged, "hidden")
	require.JSONEq(t, `[{"name":"db","type":"postgres","url":"[REDACTED]"},`+
		`{"name":"cache","options":{"endpoint":"[REDACTED]"},`+
		`"note":"see https://docs.example.com/x?token=`+redactedQueryValue+`"},`+
		`{"id":"RABC","env":"[REDACTED]","build_url":"https://ci.example.com/1"}]`, logged)
}

// An app's environment maps env var names to values: every value is redacted, whatever its name.
func TestEnvBodyRedactsEveryValue(t *testing.T) {
	logged := EnvBody([]byte(`{"RAILS_ENV":"hidden-1","DATABASE_URL":"hidden-2","PORT":"3000"}`), 1000)
	require.JSONEq(t, `{"RAILS_ENV":"[REDACTED]","DATABASE_URL":"[REDACTED]","PORT":"[REDACTED]"}`, logged)
	require.Equal(t, Redacted, strings.Trim(EnvBody([]byte(`"KEY=hidden"`), 1000), `"`))
}

// A body that can't be redacted isn't logged: the start of a long one could be cut inside a secret.
func TestBodyLeavesOutBodiesItCantRedact(t *testing.T) {
	limit := 100
	head := []byte(`{"password":"` + strings.Repeat("x", limit) + `"}`)[:limit+1]
	require.Equal(t, TooLongPlaceholder, Body(head, limit))
	require.Equal(t, TooLongPlaceholder, EnvBody(head, limit))

	for _, body := range []string{`{"password":"x"`, `password=x`, `{"a":1} {"password":"x"}`, ``} {
		require.Equal(t, NotJSONPlaceholder, Body([]byte(body), limit), body)
	}
	require.Equal(t, `{"password":"[REDACTED]"}`, Body([]byte(`{"password":"x"}`), limit))
}
