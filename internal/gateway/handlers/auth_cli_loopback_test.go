package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

const (
	loopbackTestRedirect = "http://127.0.0.1:54321/callback"
	loopbackTestVerifier = "test-verifier-0123456789-abcdefghijklmnopqrstuvwxyz-ABCDEFGHIJK"
)

// loopbackOAuth is a fake identity provider that issues a fresh gateway state per login.
type loopbackOAuth struct {
	count     int
	lastState string
	email     string
}

func (o *loopbackOAuth) StartLogin() (*auth.LoginStartResponse, error) {
	o.count++
	o.lastState = fmt.Sprintf("gateway-state-%d", o.count)
	return &auth.LoginStartResponse{
		AuthURL: "https://accounts.google.com/o/oauth2/auth", State: o.lastState,
		CodeVerifier: "google-verifier",
	}, nil
}

func (_ *loopbackOAuth) StartWebLogin() (string, string) { return "", "" }

func (o *loopbackOAuth) CompleteLogin(code, _, _ string) (*auth.LoginResponse, error) {
	if code != "google-code" {
		return nil, errors.New("invalid authorization code")
	}
	return &auth.LoginResponse{Email: o.email, Name: "Loopback User"}, nil
}

type loopbackEnv struct {
	router   *gin.Engine
	database *db.Database
	oauth    *loopbackOAuth
	sessions *auth.SessionManager
}

func newLoopbackEnv(t *testing.T, requireMFA bool) *loopbackEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	_, err := database.CreateUser("user@example.com", "Loopback User", []string{"admin"})
	require.NoError(t, err)

	sessions := auth.NewSessionManager(database, "test-secret", &auth.StaticTTLProvider{TTL: time.Hour})
	mfaService, err := mfa.NewService(database, "Rack Gateway", 30*time.Minute, 10*time.Minute,
		[]byte("pepper"), "", "", "", "", nil)
	require.NoError(t, err)
	oauth := &loopbackOAuth{email: "user@example.com"}
	handler := NewAuthHandler(oauth, database, &config.Config{DevMode: true}, sessions, mfaService,
		&db.MFASettings{RequireAllUsers: requireMFA}, nil, audit.NewLogger(database))

	router := gin.New()
	router.POST("/api/v1/auth/cli/start", handler.CLILoginStart)
	router.GET("/api/v1/auth/cli/callback", handler.CLILoginCallback)
	router.GET("/api/v1/auth/cli/mfa", handler.CLILoginMFAForm)
	router.POST("/api/v1/auth/cli/mfa", handler.CLILoginMFASubmit)
	router.GET("/api/v1/auth/cli/return", handler.CLILoginReturn)
	router.POST("/api/v1/auth/cli/cancel", handler.CLILoginCancel)
	router.POST("/api/v1/auth/cli/complete", handler.CLILoginComplete)
	return &loopbackEnv{router: router, database: database, oauth: oauth, sessions: sessions}
}

func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (e *loopbackEnv) send(
	t *testing.T,
	method, target string,
	body interface{},
	cookies ...*http.Cookie,
) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w.Result()
}

func validStartRequest() CLILoginStartRequest {
	return CLILoginStartRequest{
		CodeChallenge:       s256(loopbackTestVerifier),
		CodeChallengeMethod: "S256",
		RedirectURI:         loopbackTestRedirect,
		State:               "cli-state-abcdefghijklmnop",
		DeviceName:          "laptop.local",
	}
}

// startAndBind starts a login and completes the identity provider callback in a browser.
// It returns the gateway state and the browser's binding cookie.
func (e *loopbackEnv) startAndBind(t *testing.T) (string, *http.Cookie) {
	t.Helper()
	res := e.send(t, http.MethodPost, "/api/v1/auth/cli/start", validStartRequest())
	require.Equal(t, http.StatusOK, res.StatusCode)
	var started map[string]interface{}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&started))
	require.Equal(t, []string{"auth_url"}, keysOf(started), "start must not return the state or any verifier")

	state := e.oauth.lastState
	res = e.send(t, http.MethodGet, "/api/v1/auth/cli/callback?code=google-code&state="+state, nil)
	require.Equal(t, http.StatusTemporaryRedirect, res.StatusCode)
	binding := findCookie(res, cliLoginCookie)
	require.NotNil(t, binding)
	require.True(t, binding.HttpOnly)
	return state, binding
}

func keysOf(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// loginCodeFromBrowser follows the bound browser through MFA (none required) to the loopback redirect.
func (e *loopbackEnv) loginCodeFromBrowser(t *testing.T, state string, binding *http.Cookie) string {
	t.Helper()
	res := e.send(t, http.MethodGet, "/api/v1/auth/cli/mfa?state="+state, nil, binding)
	require.Equal(t, http.StatusTemporaryRedirect, res.StatusCode)
	require.Equal(t, cliReturnRoute(state), res.Header.Get("Location"))

	res = e.send(t, http.MethodGet, cliReturnRoute(state), nil, binding)
	require.Equal(t, http.StatusFound, res.StatusCode)
	return loopbackCode(t, res)
}

func loopbackCode(t *testing.T, res *http.Response) string {
	t.Helper()
	location, err := url.Parse(res.Header.Get("Location"))
	require.NoError(t, err)
	require.Equal(t, loopbackTestRedirect, location.Scheme+"://"+location.Host+location.Path)
	require.Equal(t, validStartRequest().State, location.Query().Get("state"))
	code := location.Query().Get("code")
	require.NotEmpty(t, code)
	return code
}

func (e *loopbackEnv) complete(t *testing.T, code, verifier string) *http.Response {
	t.Helper()
	return e.send(t, http.MethodPost, "/api/v1/auth/cli/complete", CLILoginCompleteRequest{
		LoginCode: code, CodeVerifier: verifier, DeviceName: "laptop.local",
	})
}

func TestCLILoginLoopbackFlowIssuesSingleUseCode(t *testing.T) {
	e := newLoopbackEnv(t, false)
	state, binding := e.startAndBind(t)
	code := e.loginCodeFromBrowser(t, state, binding)

	res := e.complete(t, code, loopbackTestVerifier)
	require.Equal(t, http.StatusOK, res.StatusCode)
	var login CLILoginResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&login))
	require.NotEmpty(t, login.Token)
	require.Equal(t, "user@example.com", login.Email)
	require.True(t, login.MFAVerified)

	res = e.complete(t, code, loopbackTestVerifier)
	require.Equal(t, http.StatusBadRequest, res.StatusCode, "login code must be single use")
}

func TestCLILoginCompleteRejectsWrongVerifierAndBurnsCode(t *testing.T) {
	e := newLoopbackEnv(t, false)
	state, binding := e.startAndBind(t)
	code := e.loginCodeFromBrowser(t, state, binding)

	res := e.complete(t, code, "attacker-verifier-0123456789-abcdefghijklmnopqrstuvwxyz-ABCDEF")
	require.Equal(t, http.StatusBadRequest, res.StatusCode)

	res = e.complete(t, code, loopbackTestVerifier)
	require.Equal(t, http.StatusBadRequest, res.StatusCode, "a failed redemption must consume the code")
}

func TestCLILoginBrowserStepsRequireBindingCookie(t *testing.T) {
	e := newLoopbackEnv(t, false)
	state, binding := e.startAndBind(t)

	res := e.send(t, http.MethodGet, "/api/v1/auth/cli/mfa?state="+state, nil)
	require.Equal(t, http.StatusTemporaryRedirect, res.StatusCode)
	require.Contains(t, res.Header.Get("Location"), "error=browser_mismatch")

	res = e.send(t, http.MethodPost, "/api/v1/auth/cli/mfa", map[string]string{"state": state, "code": "123456"})
	require.Equal(t, http.StatusBadRequest, res.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
	require.Equal(t, "browser_mismatch", body["error"])

	wrong := &http.Cookie{Name: cliLoginCookie, Value: "not-the-binding"}
	res = e.send(t, http.MethodGet, cliReturnRoute(state), nil, wrong)
	require.Contains(t, res.Header.Get("Location"), "error=browser_mismatch")

	// A replayed identity provider callback cannot bind a second browser.
	res = e.send(t, http.MethodGet, "/api/v1/auth/cli/callback?code=google-code&state="+state, nil)
	require.Contains(t, res.Header.Get("Location"), "error=expired")
	require.Nil(t, findCookie(res, cliLoginCookie))

	// The original browser still works.
	require.NotEmpty(t, e.loginCodeFromBrowser(t, state, binding))
}

func TestCLILoginExpiresAfterTenMinutes(t *testing.T) {
	e := newLoopbackEnv(t, false)
	state, binding := e.startAndBind(t)
	code := e.loginCodeFromBrowser(t, state, binding)

	_, err := e.database.DB().Exec(
		`UPDATE cli_login_states SET created_at = NOW() - INTERVAL '11 minutes' WHERE state = $1`, state)
	require.NoError(t, err)
	res := e.complete(t, code, loopbackTestVerifier)
	require.Equal(t, http.StatusBadRequest, res.StatusCode)
}

func TestCLILoginCodeExpires(t *testing.T) {
	e := newLoopbackEnv(t, false)
	state, binding := e.startAndBind(t)
	code := e.loginCodeFromBrowser(t, state, binding)

	_, err := e.database.DB().Exec(
		`UPDATE cli_login_states SET login_code_expires_at = NOW() - INTERVAL '1 second' WHERE state = $1`, state)
	require.NoError(t, err)
	res := e.complete(t, code, loopbackTestVerifier)
	require.Equal(t, http.StatusBadRequest, res.StatusCode)
}

func TestCLILoginUnauthorizedUserIsReturnedToCLIWithError(t *testing.T) {
	e := newLoopbackEnv(t, false)
	e.oauth.email = "stranger@example.com"
	state, binding := e.startAndBind(t)

	res := e.send(t, http.MethodGet, "/api/v1/auth/cli/mfa?state="+state, nil, binding)
	require.Equal(t, http.StatusFound, res.StatusCode)
	location, err := url.Parse(res.Header.Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:54321", location.Host)
	require.Equal(t, "unauthorized", location.Query().Get("error"))
	require.Empty(t, location.Query().Get("code"))
}

func TestCLILoginEnrollmentDuringLoginCompletesCLILogin(t *testing.T) {
	e := newLoopbackEnv(t, true)
	state, binding := e.startAndBind(t)

	res := e.send(t, http.MethodGet, "/api/v1/auth/cli/mfa?state="+state, nil, binding)
	require.Equal(t, http.StatusTemporaryRedirect, res.StatusCode)
	require.Contains(t, res.Header.Get("Location"), "enrollment=required")
	sessionCookie := findCookie(res, "session_token")
	require.NotNil(t, sessionCookie)

	// Before enrolling, the return step sends the browser back to MFA, even though the web session
	// of a not-yet-enrolled user counts as verified.
	res = e.send(t, http.MethodGet, cliReturnRoute(state), nil, binding, sessionCookie)
	require.Equal(t, cliMFARoute(state), res.Header.Get("Location"))

	// The user confirms a first factor in this browser during this login: confirming re-verifies the
	// session that did it.
	e.confirmFirstFactor(t)
	e.markSessionVerified(t, sessionCookie)

	res = e.send(t, http.MethodGet, cliReturnRoute(state), nil, binding, sessionCookie)
	require.Equal(t, http.StatusFound, res.StatusCode)
	code := loopbackCode(t, res)
	require.Equal(t, http.StatusOK, e.complete(t, code, loopbackTestVerifier).StatusCode)
}

// A first factor enrolled somewhere else (another session) doesn't satisfy the CLI login: someone
// holding the user's Google login can't just wait for the user to enroll.
func TestCLILoginEnrollmentInAnotherSessionDoesNotCompleteCLILogin(t *testing.T) {
	e := newLoopbackEnv(t, true)
	state, binding := e.startAndBind(t)
	res := e.send(t, http.MethodGet, "/api/v1/auth/cli/mfa?state="+state, nil, binding)
	sessionCookie := findCookie(res, "session_token")
	require.NotNil(t, sessionCookie)

	user, err := e.database.GetUser("user@example.com")
	require.NoError(t, err)
	otherToken, _, err := e.sessions.CreateSession(user, auth.SessionMetadata{Channel: "web"})
	require.NoError(t, err)
	e.confirmFirstFactor(t)
	e.markSessionVerified(t, &http.Cookie{Name: "session_token", Value: otherToken})

	res = e.send(t, http.MethodGet, cliReturnRoute(state), nil, binding, sessionCookie)
	require.Equal(t, cliMFARoute(state), res.Header.Get("Location"), "no login code without enrolling here")
}

func (e *loopbackEnv) confirmFirstFactor(t *testing.T) {
	t.Helper()
	user, err := e.database.GetUser("user@example.com")
	require.NoError(t, err)
	method, err := e.database.CreateMFAMethod(user.ID, "totp", "Authenticator App", "SECRET", nil, nil, nil, nil)
	require.NoError(t, err)
	require.NoError(t, e.database.ConfirmMFAMethod(method.ID, time.Now()))
}

func (e *loopbackEnv) markSessionVerified(t *testing.T, sessionCookie *http.Cookie) {
	t.Helper()
	result, err := e.sessions.ValidateSession(sessionCookie.Value, "", "")
	require.NoError(t, err)
	require.NoError(t, e.database.UpdateSessionMFAVerified(result.Session.ID, time.Now(), nil))
}

// The browser is only bound once the identity provider code exchange has succeeded, so a junk code
// from someone who learned the state can neither bind nor end the login.
func TestCLILoginJunkCodeCannotBindOrBlockLogin(t *testing.T) {
	e := newLoopbackEnv(t, false)
	res := e.send(t, http.MethodPost, "/api/v1/auth/cli/start", validStartRequest())
	require.Equal(t, http.StatusOK, res.StatusCode)
	state := e.oauth.lastState

	res = e.send(t, http.MethodGet, "/api/v1/auth/cli/callback?code=junk&state="+state, nil)
	require.Equal(t, "exchange_failed", loopbackError(t, res))
	require.Nil(t, findCookie(res, cliLoginCookie), "a failed exchange must not bind the browser")

	res = e.send(t, http.MethodGet, "/api/v1/auth/cli/callback?code=google-code&state="+state, nil)
	require.Equal(t, http.StatusTemporaryRedirect, res.StatusCode)
	binding := findCookie(res, cliLoginCookie)
	require.NotNil(t, binding)
	require.NotEmpty(t, e.loginCodeFromBrowser(t, state, binding))
}

// An identity provider error (e.g. the user declined consent) is sent back to the CLI.
func TestCLILoginProviderErrorReturnsToCLI(t *testing.T) {
	e := newLoopbackEnv(t, false)
	e.send(t, http.MethodPost, "/api/v1/auth/cli/start", validStartRequest())
	state := e.oauth.lastState

	res := e.send(t, http.MethodGet, "/api/v1/auth/cli/callback?error=access_denied&state="+state, nil)
	require.Equal(t, "access_denied", loopbackError(t, res))
	record, err := e.database.GetCLILoginState(state)
	require.NoError(t, err)
	require.False(t, record.LoginError.Valid, "an unauthenticated callback must not end the login")
}

func TestCLILoginCancelTellsTheCLI(t *testing.T) {
	e := newLoopbackEnv(t, false)
	state, binding := e.startAndBind(t)

	res := e.send(t, http.MethodPost, "/api/v1/auth/cli/cancel", map[string]string{"state": state})
	require.Equal(t, http.StatusBadRequest, res.StatusCode, "only the bound browser can cancel")

	res = e.send(t, http.MethodPost, "/api/v1/auth/cli/cancel", map[string]string{"state": state}, binding)
	require.Equal(t, http.StatusOK, res.StatusCode)
	var body CLILoginRedirectResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
	location, err := url.Parse(body.Redirect)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:54321", location.Host)
	require.Equal(t, "canceled", location.Query().Get("error"))
	require.Equal(t, validStartRequest().State, location.Query().Get("state"))

	res = e.send(t, http.MethodGet, cliReturnRoute(state), nil, binding)
	require.Equal(t, "canceled", loopbackError(t, res), "a canceled login can't be completed")
}

func loopbackError(t *testing.T, res *http.Response) string {
	t.Helper()
	require.Equal(t, http.StatusFound, res.StatusCode)
	location, err := url.Parse(res.Header.Get("Location"))
	require.NoError(t, err)
	require.Equal(t, loopbackTestRedirect, location.Scheme+"://"+location.Host+location.Path)
	require.Equal(t, validStartRequest().State, location.Query().Get("state"))
	require.Empty(t, location.Query().Get("code"))
	return location.Query().Get("error")
}

// A login code is issued once; returning to /auth/cli/return again doesn't mint another.
func TestCLILoginCodeIsIssuedOnce(t *testing.T) {
	e := newLoopbackEnv(t, false)
	state, binding := e.startAndBind(t)
	code := e.loginCodeFromBrowser(t, state, binding)

	res := e.send(t, http.MethodGet, cliReturnRoute(state), nil, binding)
	require.Contains(t, res.Header.Get("Location"), "error=expired")
	require.Equal(t, http.StatusOK, e.complete(t, code, loopbackTestVerifier).StatusCode)
}

func TestCLILoginBindingCookieAttributes(t *testing.T) {
	t.Setenv("COOKIE_SECURE", "true")
	e := newLoopbackEnv(t, false)
	_, binding := e.startAndBind(t)
	require.True(t, binding.Secure)
	require.True(t, binding.HttpOnly)
	require.Equal(t, http.SameSiteLaxMode, binding.SameSite)
	require.Equal(t, "/api/v1/auth/cli", binding.Path)
}

// CLIs from before the loopback login post no login parameters; they're told to upgrade.
func TestCLILoginStartTellsOldCLIsToUpgrade(t *testing.T) {
	e := newLoopbackEnv(t, false)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/cli/start", http.NoBody)
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "upgrade the rack-gateway CLI")
}

func TestCLILoginStartValidation(t *testing.T) {
	e := newLoopbackEnv(t, false)
	cases := map[string]func(*CLILoginStartRequest){
		"plain challenge method": func(r *CLILoginStartRequest) { r.CodeChallengeMethod = "plain" },
		"short challenge":        func(r *CLILoginStartRequest) { r.CodeChallenge = "abc" },
		"short state":            func(r *CLILoginStartRequest) { r.State = "short" },
		"remote host":            func(r *CLILoginStartRequest) { r.RedirectURI = "http://evil.example:5000/callback" },
		"localhost name":         func(r *CLILoginStartRequest) { r.RedirectURI = "http://localhost:5000/callback" },
		"https scheme": func(r *CLILoginStartRequest) {
			r.RedirectURI = "https://127.0.0.1:5000/callback"
		},
		"other path":      func(r *CLILoginStartRequest) { r.RedirectURI = "http://127.0.0.1:5000/other" },
		"query string":    func(r *CLILoginStartRequest) { r.RedirectURI = "http://127.0.0.1:5000/callback?x=1" },
		"userinfo":        func(r *CLILoginStartRequest) { r.RedirectURI = "http://a@127.0.0.1:5000/callback" },
		"privileged port": func(r *CLILoginStartRequest) { r.RedirectURI = "http://127.0.0.1:80/callback" },
		"no port":         func(r *CLILoginStartRequest) { r.RedirectURI = "http://127.0.0.1/callback" },
	}
	for name, mutate := range cases {
		req := validStartRequest()
		mutate(&req)
		res := e.send(t, http.MethodPost, "/api/v1/auth/cli/start", req)
		require.Equalf(t, http.StatusBadRequest, res.StatusCode, name)
	}

	ipv6 := validStartRequest()
	ipv6.RedirectURI = "http://[::1]:5000/callback"
	require.Equal(t, http.StatusOK, e.send(t, http.MethodPost, "/api/v1/auth/cli/start", ipv6).StatusCode)
}

func TestSanitizeDeviceName(t *testing.T) {
	require.Equal(t, "laptop.local", sanitizeDeviceName("  laptop.local\n"))
	require.Equal(t, "Approve bITb login", sanitizeDeviceName("Approve <b>IT</b> login"))
	require.Len(t, sanitizeDeviceName(strings.Repeat("a", 200)), cliMaxDeviceNameLen)
}
