package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

const (
	protectedEnvReleasesPath = "/apps/app/releases"
	// protectedEnvRackEnv is the env of the fake rack's latest release. DATABASE_URL and ADMIN_PASSWORD are
	// protected for app; neither is a secret by name, so only protection keeps their values out of the logs.
	protectedEnvRackEnv = "DATABASE_URL=postgres://old\nADMIN_PASSWORD=hunter2\nPORT=3000"
	// protectedEnvCLIPost is what `env set DATABASE_URL=postgres://new` posts: the CLI fetched the env with
	// ADMIN_PASSWORD (and DATABASE_URL) masked and changed DATABASE_URL.
	protectedEnvCLIPost = "DATABASE_URL=postgres://new\nADMIN_PASSWORD=********************\nPORT=3000"
	stepUpAgeFresh      = 2 * time.Minute  // inside the default 10-minute step-up window
	stepUpAgeExpired    = 30 * time.Minute // outside it
)

// protectedEnvRack is a fake rack with one release, whose env is env (protectedEnvRackEnv by default). It records
// every release created through it.
type protectedEnvRack struct {
	mu      sync.Mutex
	env     string
	created []createdRelease
}

type createdRelease struct {
	env    string
	header http.Header
}

func (rk *protectedEnvRack) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == protectedEnvReleasesPath:
		_, _ = w.Write([]byte(`[{"id":"R1"}]`))
	case r.Method == http.MethodGet && r.URL.Path == protectedEnvReleasesPath+"/R1":
		rk.mu.Lock()
		body, _ := json.Marshal(map[string]string{"id": "R1", "env": rk.env})
		rk.mu.Unlock()
		_, _ = w.Write(body)
	case r.Method == http.MethodPost && r.URL.Path == protectedEnvReleasesPath:
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		_ = r.ParseForm()
		rk.mu.Lock()
		rk.created = append(rk.created, createdRelease{env: r.PostForm.Get("env"), header: r.Header.Clone()})
		rk.mu.Unlock()
		_, _ = w.Write([]byte(`{"id":"R2"}`))
	default:
		http.NotFound(w, r)
	}
}

// setEnv replaces the env of the rack's release.
func (rk *protectedEnvRack) setEnv(env string) {
	rk.mu.Lock()
	defer rk.mu.Unlock()
	rk.env = env
}

func (rk *protectedEnvRack) releases() []createdRelease {
	rk.mu.Lock()
	defer rk.mu.Unlock()
	return append([]createdRelease(nil), rk.created...)
}

// protectedEnvFixture is a proxy with MFA enforced as in production, in front of a protectedEnvRack.
type protectedEnvFixture struct {
	h        *Handler
	database *db.Database
	rack     *protectedEnvRack
	sessions *auth.SessionManager
	mfa      *mfa.Service
	admin    *db.User
	deployer *db.User
	totpKey  *otp.Key
}

func newProtectedEnvFixture(t *testing.T) *protectedEnvFixture {
	t.Helper()
	t.Setenv("CONVOX_SECRET_ENV_VARS", "") // mise.toml names DATABASE_URL a secret; here only protection applies
	rack := &protectedEnvRack{env: protectedEnvRackEnv}
	server := httptest.NewServer(http.HandlerFunc(rack.handler))
	t.Cleanup(server.Close)

	database := dbtest.NewDatabase(t)
	mgr, err := rbac.NewDBManager(database, "example.com")
	require.NoError(t, err)
	sessions := auth.NewSessionManager(database, "test-secret", &auth.StaticTTLProvider{TTL: time.Hour})
	mfaService, err := mfa.NewService(
		database, "Test", 24*time.Hour, 10*time.Minute, []byte("test-pepper"), "", "", "", "", nil,
	)
	require.NoError(t, err)
	cfg := &config.Config{Racks: map[string]config.RackConfig{
		"default": {Name: "default", URL: server.URL, Username: "convox", APIKey: "token", Enabled: true},
	}}
	h := NewHandler(
		cfg, mgr, audit.NewLogger(database), database, settings.NewService(database), email.NoopSender{},
		"default", "default", nil, mfaService, sessions,
	)

	app := "app"
	require.NoError(t, database.UpsertSetting(
		&app, "protected_env_vars", []string{"DATABASE_URL", "ADMIN_PASSWORD"}, nil,
	))
	admin, err := database.CreateUser("admin@test.com", "Admin", []string{"admin"})
	require.NoError(t, err)
	deployer, err := database.CreateUser("deployer@test.com", "Deployer", []string{"deployer"})
	require.NoError(t, err)
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "Test", AccountName: admin.Email})
	require.NoError(t, err)
	method, err := database.CreateMFAMethod(admin.ID, "totp", "Authenticator", key.Secret(), nil, nil, nil, nil)
	require.NoError(t, err)
	require.NoError(t, database.ConfirmMFAMethod(method.ID, time.Now()))

	return &protectedEnvFixture{
		h: h, database: database, rack: rack, sessions: sessions, mfa: mfaService,
		admin: admin, deployer: deployer, totpKey: key,
	}
}

func (f *protectedEnvFixture) totpCode(t *testing.T) string {
	t.Helper()
	code, err := totp.GenerateCode(f.totpKey.Secret(), time.Now())
	require.NoError(t, err)
	return code
}

// request is a release create posting env as user, from a CLI session that completed MFA at login and last
// stepped up stepUpAge ago. optIn is the X-Allow-Protected-Env header and mfaCode the inline TOTP code the CLI
// embeds in its credential; "" omits either.
func (f *protectedEnvFixture) request(
	t *testing.T,
	user *db.User,
	env, optIn, mfaCode string,
	stepUpAge time.Duration,
) (*http.Request, *db.UserSession) {
	t.Helper()
	_, session, err := f.sessions.CreateSession(user, auth.SessionMetadata{Channel: "cli"})
	require.NoError(t, err)
	now := time.Now()
	stepUpAt := now.Add(-stepUpAge)
	session.MFAVerifiedAt = &now
	session.RecentStepUpAt = &stepUpAt

	authUser := &auth.User{Email: user.Email, Name: user.Name, Roles: user.Roles, DBUser: user, Session: session}
	if mfaCode != "" {
		authUser.MFAType, authUser.MFAValue = "totp", mfaCode
	}
	return withEnvRequest(authUser, env, optIn), session
}

// withEnvRequest is a release create posting env for authUser, with optIn as X-Allow-Protected-Env ("" omits it).
func withEnvRequest(authUser *auth.User, env, optIn string) *http.Request {
	body := url.Values{"env": {env}}.Encode()
	req := httptest.NewRequest(http.MethodPost, protectedEnvReleasesPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if optIn != "" {
		req.Header.Set(envutil.AllowProtectedHeader, optIn)
	}
	return req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, authUser))
}

func (f *protectedEnvFixture) proxy(req *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	f.h.ProxyToRack(rr, req)
	return rr
}

// databaseURLAudit returns userEmail's audit entry with action on app/DATABASE_URL.
func (f *protectedEnvFixture) databaseURLAudit(t *testing.T, userEmail, action string) *db.AuditLog {
	t.Helper()
	logs, err := f.database.GetAuditLogs(userEmail, time.Time{}, 100)
	require.NoError(t, err)
	for _, entry := range logs {
		if entry.Action == action && entry.Resource == "app/DATABASE_URL" {
			return entry
		}
	}
	t.Fatalf("no %s audit entry for app/DATABASE_URL", action)
	return nil
}

var (
	envSetAction         = audit.BuildAction(rbac.ResourceEnv.String(), rbac.ActionSet.String())
	setProtectedAction   = audit.BuildAction(rbac.ResourceEnv.String(), rbac.ActionSetProtected.String())
	unsetProtectedAction = audit.BuildAction(rbac.ResourceEnv.String(), audit.ActionVerbUnsetProtected)
)

// Without naming the key in X-Allow-Protected-Env, a protected change is refused as before, even for an admin
// with fresh MFA. Naming a different protected key doesn't help.
func TestProtectedEnvChangeWithoutOptInIsRefused(t *testing.T) {
	f := newProtectedEnvFixture(t)
	for _, optIn := range []string{"", "ADMIN_PASSWORD"} {
		req, _ := f.request(t, f.admin, protectedEnvCLIPost, optIn, f.totpCode(t), stepUpAgeFresh)
		rr := f.proxy(req)
		require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
		require.Contains(t, rr.Body.String(), "DATABASE_URL is a protected env var for app. "+
			"An admin can change it with --allow-protected and MFA, or unprotect it in rack-gateway settings.")
	}
	require.Empty(t, f.rack.releases(), "a refused change must not reach the rack")

	entry := f.databaseURLAudit(t, f.admin.Email, envSetAction)
	require.Equal(t, "denied", entry.Status)
	require.Equal(t, http.StatusForbidden, entry.HTTPStatus)
}

func TestProtectedEnvChangeNeedsSetProtectedPermission(t *testing.T) {
	f := newProtectedEnvFixture(t)
	req, _ := f.request(t, f.deployer, protectedEnvCLIPost, "DATABASE_URL", "123456", stepUpAgeFresh)
	rr := f.proxy(req)
	require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "You don't have permission to change protected env vars: DATABASE_URL")
	require.Empty(t, f.rack.releases())

	entry := f.databaseURLAudit(t, f.deployer.Email, setProtectedAction)
	require.Equal(t, "denied", entry.Status)
}

// API tokens can't do MFA, so they never change protected env vars, whatever their permissions.
func TestProtectedEnvChangeRefusedForAPITokens(t *testing.T) {
	f := newProtectedEnvFixture(t)
	tokenID := int64(99)
	tokenUser := &auth.User{
		Email: f.admin.Email, IsAPIToken: true, TokenID: &tokenID, TokenName: "ci",
		Permissions: []string{"convox:*:*"}, DBUser: f.admin,
	}
	rr := f.proxy(withEnvRequest(tokenUser, protectedEnvCLIPost, "DATABASE_URL"))
	require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "API tokens can't change protected env vars: DATABASE_URL")
	require.Empty(t, f.rack.releases())

	entry := f.databaseURLAudit(t, f.admin.Email, setProtectedAction)
	require.Equal(t, "denied", entry.Status)
}

// An open step-up window is not enough: the request itself must carry MFA.
func TestProtectedEnvChangeRequiresMFAOnTheRequest(t *testing.T) {
	f := newProtectedEnvFixture(t)
	req, _ := f.request(t, f.admin, protectedEnvCLIPost, "DATABASE_URL", "", stepUpAgeFresh)
	rr := f.proxy(req)
	require.Equal(t, http.StatusUnauthorized, rr.Code, rr.Body.String())
	require.Equal(t, "true", rr.Header().Get("X-MFA-Required"))
	require.Contains(t, rr.Body.String(), "Multi-factor authentication is required to change protected env vars")
	require.Empty(t, f.rack.releases())

	entry := f.databaseURLAudit(t, f.admin.Email, setProtectedAction)
	require.Equal(t, "denied", entry.Status)
	require.Equal(t, http.StatusUnauthorized, entry.HTTPStatus)
}

// Within the step-up window the proxy normally skips the inline proof; for a protected change it verifies it.
func TestProtectedEnvChangeRejectsWrongMFACodeWithinStepUpWindow(t *testing.T) {
	f := newProtectedEnvFixture(t)
	code := f.totpCode(t)
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	req, _ := f.request(t, f.admin, protectedEnvCLIPost, "DATABASE_URL", wrong, stepUpAgeFresh)
	rr := f.proxy(req)
	require.Equal(t, http.StatusUnauthorized, rr.Code, rr.Body.String())
	require.Equal(t, "true", rr.Header().Get("X-MFA-Required"))
	require.Empty(t, f.rack.releases())
}

func TestProtectedEnvChangeVerifiesMFAWithinStepUpWindow(t *testing.T) {
	f := newProtectedEnvFixture(t)
	code := f.totpCode(t)
	req, session := f.request(t, f.admin, protectedEnvCLIPost, "database_url", code, stepUpAgeFresh)
	rr := f.proxy(req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	releases := f.rack.releases()
	require.Len(t, releases, 1)
	require.Contains(t, releases[0].env, "DATABASE_URL=postgres://new")
	require.Contains(t, releases[0].env, "ADMIN_PASSWORD=hunter2", "the masked protected key keeps its value")
	require.Empty(t, releases[0].header.Get(envutil.AllowProtectedHeader), "the opt-in is never forwarded")

	// The inline code was verified (and consumed) on this request, and the step-up window restarted.
	_, err := f.mfa.VerifyTOTP(f.admin, code, "127.0.0.1", "test", nil)
	require.Error(t, err, "the code must already have been used")
	stored, err := f.database.GetSessionByID(session.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.RecentStepUpAt)
	require.WithinDuration(t, time.Now(), *stored.RecentStepUpAt, time.Minute)

	entry := f.databaseURLAudit(t, f.admin.Email, setProtectedAction)
	require.Equal(t, "success", entry.Status)
	require.Equal(t, "env", entry.ResourceType)
	require.Equal(t, `{"old":"[REDACTED]","new":"[REDACTED]"}`, entry.Details)
}

// With the step-up window expired, the proxy's step-up check verifies the inline code. The protected check must
// not verify it again: TOTP replay protection would reject it.
func TestProtectedEnvChangeVerifiesMFAOnceWhenStepUpExpired(t *testing.T) {
	f := newProtectedEnvFixture(t)
	req, _ := f.request(t, f.admin, protectedEnvCLIPost, "DATABASE_URL", f.totpCode(t), stepUpAgeExpired)
	rr := f.proxy(req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Len(t, f.rack.releases(), 1)
}

// `env unset --allow-protected DATABASE_URL` leaves DATABASE_URL out of the posted env. The opted-in key is
// removed; a protected key the request leaves out without naming it is kept.
func TestProtectedEnvUnsetRemovesOnlyOptedInKeys(t *testing.T) {
	f := newProtectedEnvFixture(t)
	req, _ := f.request(t, f.admin, "PORT=3000", "DATABASE_URL", f.totpCode(t), stepUpAgeFresh)
	rr := f.proxy(req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	releases := f.rack.releases()
	require.Len(t, releases, 1)
	require.NotContains(t, releases[0].env, "DATABASE_URL")
	require.Contains(t, releases[0].env, "ADMIN_PASSWORD=hunter2")
	require.Contains(t, releases[0].env, "PORT=3000")

	entry := f.databaseURLAudit(t, f.admin.Email, unsetProtectedAction)
	require.Equal(t, "success", entry.Status)
	require.Equal(t, `{"old":"[REDACTED]","new":"[REDACTED]"}`, entry.Details)
}

// Protected values stay redacted even when audit_visible_env_vars lists their keys.
func TestLogEnvDiffsRedactsProtectedValues(t *testing.T) {
	h, database, _ := newProxyForEnvTest(t)
	app := "my-app"
	require.NoError(t, database.UpsertSetting(
		&app, settings.KeyAuditVisibleEnvVars, []string{"ADMIN_PASSWORD", "DATABASE_URL", "PORT"}, nil,
	))
	req := httptest.NewRequest(http.MethodPost, "/apps/my-app/releases", nil)
	h.logEnvDiffs(req, "admin@test.com", "default", []envutil.EnvDiff{
		{Key: "ADMIN_PASSWORD", OldVal: "old-value", NewVal: "new-value", Protected: true},
		{Key: "DATABASE_URL", OldVal: "postgres://old", NewVal: "", Protected: true},
		{Key: "PORT", OldVal: "3000", NewVal: "4000"},
	})

	logs, err := database.GetAuditLogs("admin@test.com", time.Time{}, 10)
	require.NoError(t, err)
	got := map[string]*db.AuditLog{}
	for _, entry := range logs {
		got[entry.Resource] = entry
	}
	require.Len(t, got, 3)
	require.Equal(t, setProtectedAction, got["my-app/ADMIN_PASSWORD"].Action)
	require.Equal(t, unsetProtectedAction, got["my-app/DATABASE_URL"].Action)
	require.Equal(t, envSetAction, got["my-app/PORT"].Action)
	for _, resource := range []string{"my-app/ADMIN_PASSWORD", "my-app/DATABASE_URL"} {
		require.Equal(t, `{"old":"[REDACTED]","new":"[REDACTED]"}`, got[resource].Details, resource)
	}
	require.Equal(t, `{"old":"3000","new":"4000"}`, got["my-app/PORT"].Details)
}

func TestOptedInProtectedKeysParsesHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, protectedEnvReleasesPath, nil)
	req.Header.Add(envutil.AllowProtectedHeader, " database_url, ,ADMIN_PASSWORD")
	req.Header.Add(envutil.AllowProtectedHeader, "Api_Key")
	keys := optedInProtectedKeys(req)
	for _, key := range []string{"DATABASE_URL", "admin_password", " API_KEY "} {
		require.Truef(t, keys.has(key), "%s should be opted in", key)
	}
	require.False(t, keys.has("PORT"))
	require.Len(t, keys, 3)
}
