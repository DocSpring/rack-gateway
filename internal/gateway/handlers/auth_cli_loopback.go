package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// The CLI login is an RFC 8252 loopback flow:
//
//  1. The CLI listens on 127.0.0.1, then calls /auth/cli/start with an S256 code challenge, its own
//     state and its loopback redirect URI. It receives only the identity provider URL.
//  2. The browser that completes the identity provider login is bound to the login with an HttpOnly
//     cookie. Every later browser step (MFA form, MFA submit, return) requires that cookie.
//  3. Once MFA is satisfied, /auth/cli/return redirects the browser to the CLI's loopback listener with
//     a single-use login code and the CLI's state.
//  4. The CLI redeems the login code together with its code verifier at /auth/cli/complete.
//
// A login link sent to someone else delivers its login code to their own machine, never to the
// person who started it, and the gateway's own OAuth state is never a credential.
const (
	cliLoginCookie       = "rgw_cli_login"
	cliLoginCookiePath   = "/api/v1/auth/cli"
	cliLoginCookieMaxAge = 10 * 60
	cliChallengeMethod   = "S256"
	cliMinLoopbackPort   = 1024
	cliMaxLoopbackPort   = 65535
	cliMaxDeviceNameLen  = 64
)

var (
	cliChallengePattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	cliStatePattern       = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
	cliDeviceUnsafeChars  = regexp.MustCompile(`[^A-Za-z0-9._ -]`)
	errInvalidRedirectURI = errors.New("redirect_uri must be http://127.0.0.1:<port>/callback")
)

// validateCLIStart checks the CLI's loopback login parameters and returns a display-safe device name.
func validateCLIStart(req CLILoginStartRequest) (string, error) {
	if req.CodeChallengeMethod != cliChallengeMethod {
		return "", errors.New("code_challenge_method must be S256")
	}
	if !cliChallengePattern.MatchString(req.CodeChallenge) {
		return "", errors.New("code_challenge must be a base64url SHA-256 digest")
	}
	if !cliStatePattern.MatchString(req.State) {
		return "", errors.New("state must be 16-128 base64url characters")
	}
	if err := validateLoopbackRedirectURI(req.RedirectURI); err != nil {
		return "", err
	}
	return sanitizeDeviceName(req.DeviceName), nil
}

// validateLoopbackRedirectURI accepts only http://127.0.0.1:<port>/callback or http://[::1]:<port>/callback.
func validateLoopbackRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.String() != raw {
		return errInvalidRedirectURI
	}
	if u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/callback" {
		return errInvalidRedirectURI
	}
	if host := u.Hostname(); host != "127.0.0.1" && host != "::1" {
		return errInvalidRedirectURI
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < cliMinLoopbackPort || port > cliMaxLoopbackPort {
		return errInvalidRedirectURI
	}
	return nil
}

func sanitizeDeviceName(name string) string {
	cleaned := strings.TrimSpace(cliDeviceUnsafeChars.ReplaceAllString(name, ""))
	if len(cleaned) > cliMaxDeviceNameLen {
		cleaned = cleaned[:cliMaxDeviceNameLen]
	}
	return cleaned
}

// pkceS256Matches reports whether S256(verifier) equals the stored challenge, in constant time.
func pkceS256Matches(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}

// newCLISecret returns a random 256-bit base64url value.
func newCLISecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashCLISecret returns the hex SHA-256 of a secret; only hashes are stored.
func hashCLISecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func (h *AuthHandler) setCLILoginCookie(c *gin.Context, value string, maxAge int) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(cliLoginCookie, value, maxAge, cliLoginCookiePath, "", h.cookieSecure(), true)
	c.SetSameSite(http.SameSiteDefaultMode)
}

// browserBoundTo reports whether this request comes from the browser bound to the login.
func browserBoundTo(c *gin.Context, record *db.CLILoginState) bool {
	cookie, err := c.Cookie(cliLoginCookie)
	if err != nil || strings.TrimSpace(cookie) == "" || !record.BrowserBindingHash.Valid {
		return false
	}
	computed := hashCLISecret(strings.TrimSpace(cookie))
	return subtle.ConstantTimeCompare([]byte(computed), []byte(record.BrowserBindingHash.String)) == 1
}

// loadBoundCLILogin loads a live CLI login for the bound browser. It returns an error code
// suitable for the MFA challenge page when the login is missing, expired or not bound to this browser.
func (h *AuthHandler) loadBoundCLILogin(c *gin.Context, state string) (*db.CLILoginState, string) {
	if state == "" {
		return nil, "missing_state"
	}
	if h.database == nil {
		return nil, "service_unavailable"
	}
	record, err := h.database.GetCLILoginState(state)
	if err != nil {
		return nil, "load_failure"
	}
	if record == nil {
		return nil, "expired"
	}
	if !browserBoundTo(c, record) {
		return nil, "browser_mismatch"
	}
	return record, ""
}

// liveBoundCLILogin loads the CLI login bound to this browser for a browser step. When the login is
// missing, unbound or already failed, it ends the request (redirecting with the error) and returns false.
func (h *AuthHandler) liveBoundCLILogin(c *gin.Context, state string) (*db.CLILoginState, bool) {
	record, errCode := h.loadBoundCLILogin(c, state)
	if errCode != "" {
		cliRedirectWithError(c, errCode)
		return nil, false
	}
	if record.LoginError.Valid {
		h.failCLILogin(c, record, strings.TrimSpace(record.LoginError.String))
		return nil, false
	}
	return record, true
}

func cliReturnRoute(state string) string {
	return APIRoute("auth/cli/return") + "?state=" + url.QueryEscape(state)
}

func cliMFARoute(state string) string {
	return APIRoute("auth/cli/mfa") + "?state=" + url.QueryEscape(state)
}

// cliLoopbackURL is the CLI's loopback listener URL (validated at /auth/cli/start) with the CLI's state.
func cliLoopbackURL(record *db.CLILoginState, params url.Values) string {
	params.Set("state", record.CLIState)
	return record.CLIRedirectURI + "?" + params.Encode()
}

// redirectToCLI sends the browser to the CLI's loopback listener with the CLI's state.
func redirectToCLI(c *gin.Context, record *db.CLILoginState, params url.Values) {
	c.Redirect(http.StatusFound, cliLoopbackURL(record, params))
}

// returnErrorToCLI tells a CLI on the same machine as this browser that the login failed, without
// recording the failure: the request isn't proven to come from the browser bound to the login.
func returnErrorToCLI(c *gin.Context, record *db.CLILoginState, errorCode string) {
	redirectToCLI(c, record, url.Values{"error": {errorCode}})
}

// failCLILogin ends the login and tells the waiting CLI why. Only call it for the browser bound to the
// login (or one that has just been bound).
func (h *AuthHandler) failCLILogin(c *gin.Context, record *db.CLILoginState, errorCode string) {
	h.endCLILogin(record, errorCode, c)
	returnErrorToCLI(c, record, errorCode)
}

// endCLILogin records a terminal error on the login and clears the browser's binding cookie.
func (h *AuthHandler) endCLILogin(record *db.CLILoginState, errorCode string, c *gin.Context) {
	if h.database != nil {
		_ = h.database.FailCLILoginState(record.State, errorCode)
	}
	h.setCLILoginCookie(c, "", -1)
}

// CLILoginCancel godoc
// @Summary Cancel a CLI login
// @Description Ends the CLI login from the browser bound to it and returns the URL that tells the waiting CLI.
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body CLILoginCancelRequest true "Login state"
// @Success 200 {object} CLILoginRedirectResponse
// @Failure 400 {object} ErrorResponse
// @Router /auth/cli/cancel [post]
func (h *AuthHandler) CLILoginCancel(c *gin.Context) {
	var req CLILoginCancelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	record, errCode := h.loadBoundCLILogin(c, strings.TrimSpace(req.State))
	if errCode != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": errCode})
		return
	}
	h.endCLILogin(record, "canceled", c)
	redirect := cliLoopbackURL(record, url.Values{"error": {"canceled"}})
	c.JSON(http.StatusOK, CLILoginRedirectResponse{Redirect: redirect})
}

// CLILoginReturn godoc
// @Summary Return the browser to the CLI
// @Description Issues a single-use login code and redirects the bound browser to the CLI's loopback listener.
// @Tags Auth
// @Param state query string true "Login state"
// @Success 302 {string} string "Found"
// @Router /auth/cli/return [get]
func (h *AuthHandler) CLILoginReturn(c *gin.Context) {
	state := strings.TrimSpace(c.Query("state"))
	record, ok := h.liveBoundCLILogin(c, state)
	if !ok {
		return
	}
	if !record.LoginEmail.Valid || !h.cliMFASatisfied(c, record) {
		c.Redirect(http.StatusFound, cliMFARoute(state))
		return
	}

	loginCode, err := newCLISecret()
	if err != nil {
		h.failCLILogin(c, record, "persist_failure")
		return
	}
	stored, err := h.database.SetCLILoginCode(state, hashCLISecret(loginCode))
	if err != nil {
		h.failCLILogin(c, record, "persist_failure")
		return
	}
	if !stored {
		// A login code was already issued for this login (codes are issued once).
		cliRedirectWithError(c, "expired")
		return
	}
	h.setCLILoginCookie(c, "", -1)
	redirectToCLI(c, record, url.Values{"code": {loginCode}})
}

// cliMFASatisfied reports whether the login's MFA requirement is met. A user who had no factor when
// the login reached MFA satisfies it by enrolling one during this login, in this browser: confirming a
// new factor requires a valid code from it, and re-verifies the session that confirmed it (enrolling a
// first factor clears every other session's MFA state).
func (h *AuthHandler) cliMFASatisfied(c *gin.Context, record *db.CLILoginState) bool {
	if record.MFAVerifiedAt.Valid {
		return true
	}
	if !record.EnrollmentRequired {
		return false
	}
	user, err := h.database.GetUser(record.LoginEmail.String)
	if err != nil || user == nil {
		return false
	}
	enrolledAt, ok := h.enrolledDuringLogin(user, record)
	if !ok {
		return false
	}
	session := h.cliBrowserSession(c, user)
	if session == nil || session.MFAVerifiedAt == nil || session.MFAVerifiedAt.Before(enrolledAt) {
		return false
	}
	return h.database.MarkCLILoginVerified(record.State, nil) == nil
}

// enrolledDuringLogin reports whether every confirmed factor the user has was confirmed after the
// login started (and there is at least one), i.e. the user had none before this login. It returns
// when the most recent factor was confirmed.
func (h *AuthHandler) enrolledDuringLogin(user *db.User, record *db.CLILoginState) (time.Time, bool) {
	methods, err := h.database.ListMFAMethods(user.ID)
	if err != nil || len(methods) == 0 {
		return time.Time{}, false
	}
	var latest time.Time
	for _, method := range methods {
		if method.ConfirmedAt == nil || method.ConfirmedAt.Before(record.CreatedAt) {
			return time.Time{}, false
		}
		if method.ConfirmedAt.After(latest) {
			latest = *method.ConfirmedAt
		}
	}
	return latest, true
}
