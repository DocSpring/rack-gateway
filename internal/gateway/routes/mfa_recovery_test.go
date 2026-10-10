package routes_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// startCLILogin creates a CLI login whose browser has been bound after a successful identity provider
// exchange for the user, as the callback leaves it before the MFA challenge. It returns the
// browser-binding cookie value.
func (e *mfaRouteEnv) startCLILogin(t *testing.T, state string, user *db.User) string {
	t.Helper()
	err := e.database.CreateCLILoginState(db.NewCLILogin{
		State:             state,
		OAuthCodeVerifier: "verifier",
		CLICodeChallenge:  "challenge",
		CLIRedirectURI:    "http://127.0.0.1:43123/callback",
		CLIState:          "cli-" + state,
	})
	if err != nil {
		t.Fatal(err)
	}
	binding := "binding-" + state
	sum := sha256.Sum256([]byte(binding))
	bound, err := e.database.BindCLILoginBrowser(state, hex.EncodeToString(sum[:]), user.Email, user.Name)
	if err != nil || !bound {
		t.Fatalf("bind CLI login browser: bound=%v err=%v", bound, err)
	}
	return binding
}

func cliMFABody(state, code string) string {
	return fmt.Sprintf(`{"state":%q,"method":"totp","code":%q}`, state, code)
}

// boundBrowser sends the browser's session cookie and the CLI login's binding cookie, as the browser
// that approved the login does.
func boundBrowser(s *webSession, binding string) map[string]string {
	return map[string]string{"Cookie": s.headers["Cookie"] + "; rgw_cli_login=" + binding}
}

func (e *mfaRouteEnv) sessionVerified(t *testing.T, s *webSession) bool {
	t.Helper()
	result, err := e.sessions.ValidateSession(s.token, "10.1.2.3", "mfa-route-test")
	if err != nil {
		t.Fatal(err)
	}
	return result.Session.MFAVerifiedAt != nil
}

func (e *mfaRouteEnv) backupCodes(t *testing.T) []string {
	t.Helper()
	codes, err := e.mfaService.GenerateBackupCodes(e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	return codes
}

// typedBackupCode formats a backup code the way a person might type it.
func typedBackupCode(code string) string {
	return strings.ToLower(code[:6]) + "-" + strings.ToLower(code[6:])
}

func TestCLILoginMFAVerifiesBrowserSession(t *testing.T) {
	e := newMFARouteEnv(t, "cli-browser@example.com")
	browser := e.newSession(t, e.user)
	binding := e.startCLILogin(t, "cli-state-browser", e.user)

	w := e.do("POST", "/api/v1/auth/cli/mfa", cliMFABody("cli-state-browser", e.currentCode(t)),
		boundBrowser(browser, binding))
	assertStatus(t, w, http.StatusOK, "CLI MFA submit")

	if !e.sessionVerified(t, browser) {
		t.Fatal("expected the CLI login MFA to verify the browser session")
	}
	assertStatus(t, e.do("GET", "/api/v1/users", "", browser.headers), http.StatusOK, "web UI after CLI login")
}

func TestCLILoginMFALeavesOtherUsersSessionsPending(t *testing.T) {
	e := newMFARouteEnv(t, "cli-owner@example.com")
	bystander, err := e.database.CreateUser("bystander@example.com", "Bystander", []string{"admin"})
	if err != nil {
		t.Fatal(err)
	}
	browser := e.newSession(t, bystander)
	binding := e.startCLILogin(t, "cli-state-other", e.user)

	w := e.do("POST", "/api/v1/auth/cli/mfa", cliMFABody("cli-state-other", e.currentCode(t)),
		boundBrowser(browser, binding))
	assertStatus(t, w, http.StatusOK, "CLI MFA submit")

	if e.sessionVerified(t, browser) {
		t.Fatal("a CLI login for one user must not verify another user's browser session")
	}
}

func TestInfoReportsPendingMFASession(t *testing.T) {
	e := newMFARouteEnv(t, "info-pending@example.com")
	s := e.newSession(t, e.user)

	pending := func(label string) bool {
		w := e.do("GET", "/api/v1/info", "", s.headers)
		assertStatus(t, w, http.StatusOK, label)
		var resp struct {
			User struct {
				MFAPending bool `json:"mfa_pending"`
			} `json:"user"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp.User.MFAPending
	}

	if !pending("info before MFA") {
		t.Fatal("expected mfa_pending for a session that has not completed MFA")
	}
	e.markVerified(t, s, true)
	if pending("info after MFA") {
		t.Fatal("expected mfa_pending to clear once the session completed MFA")
	}
}

func TestBackupCodeCompletesWebChallengeOnce(t *testing.T) {
	e := newMFARouteEnv(t, "backup-web@example.com")
	codes := e.backupCodes(t)
	first := e.newSession(t, e.user)

	body := fmt.Sprintf(`{"code":%q}`, typedBackupCode(codes[0]))
	assertStatus(t, e.do("POST", "/api/v1/auth/mfa/verify", body, first.headers), http.StatusOK, "verify backup code")
	assertStatus(t, e.do("GET", "/api/v1/users", "", first.headers), http.StatusOK, "web UI after backup code")

	second := e.newSession(t, e.user)
	body = fmt.Sprintf(`{"code":%q}`, codes[0])
	assertStatus(t, e.do("POST", "/api/v1/auth/mfa/verify", body, second.headers), http.StatusBadRequest,
		"reused backup code")
	if e.sessionVerified(t, second) {
		t.Fatal("a reused backup code must not verify the session")
	}
}

func TestBackupCodeCompletesPendingSessionInline(t *testing.T) {
	e := newMFARouteEnv(t, "backup-inline@example.com")
	codes := e.backupCodes(t)
	s := e.newSession(t, e.user)

	headers := withHeader(s.headers, "X-MFA-TOTP", typedBackupCode(codes[0]))
	assertStatus(t, e.do("GET", "/api/v1/users", "", headers), http.StatusOK, "inline backup code")
	if !e.sessionVerified(t, s) {
		t.Fatal("expected the inline backup code to verify the session")
	}
}

func TestBackupCodeApprovesCLILoginOnce(t *testing.T) {
	e := newMFARouteEnv(t, "backup-cli@example.com")
	codes := e.backupCodes(t)
	browser := e.newSession(t, e.user)

	binding := e.startCLILogin(t, "cli-state-backup-1", e.user)
	w := e.do("POST", "/api/v1/auth/cli/mfa", cliMFABody("cli-state-backup-1", typedBackupCode(codes[0])),
		boundBrowser(browser, binding))
	assertStatus(t, w, http.StatusOK, "CLI MFA submit with backup code")

	binding = e.startCLILogin(t, "cli-state-backup-2", e.user)
	w = e.do("POST", "/api/v1/auth/cli/mfa", cliMFABody("cli-state-backup-2", codes[0]), boundBrowser(browser, binding))
	assertStatus(t, w, http.StatusBadRequest, "CLI MFA submit with reused backup code")
	if !strings.Contains(w.Body.String(), "invalid_code") {
		t.Fatalf("expected invalid_code, got %s", w.Body.String())
	}
}
