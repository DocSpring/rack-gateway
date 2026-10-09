package routes_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp/totp"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/deps"
	"github.com/DocSpring/rack-gateway/internal/gateway/proxy"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/routes"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/DocSpring/rack-gateway/internal/gateway/token"
)

const mfaTestHost = "gateway.example.com"

type mfaRouteEnv struct {
	router     *gin.Engine
	database   *db.Database
	sessions   *auth.SessionManager
	mfaService *mfa.Service
	user       *db.User
	totpSecret string
}

// newMFARouteEnv builds the full router against a fresh test database. When enrolledEmail is set,
// an admin user with a confirmed TOTP factor is created before the RBAC manager loads users.
func newMFARouteEnv(t *testing.T, enrolledEmail string) *mfaRouteEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)

	rack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(rack.Close)

	sessions := auth.NewSessionManager(database, "test-secret", &auth.StaticTTLProvider{TTL: time.Hour})
	mfaService, err := mfa.NewService(database, "RG", 30*24*time.Hour, 10*time.Minute, []byte("pepper"),
		"", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	env := &mfaRouteEnv{database: database, sessions: sessions, mfaService: mfaService}
	if enrolledEmail != "" {
		env.enrollUser(t, enrolledEmail)
	}

	rbacManager, err := rbac.NewDBManager(database, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	tokenService := token.NewService(database)
	cfg := &config.Config{
		Domain:        mfaTestHost,
		SessionSecret: "test-secret",
		Racks: map[string]config.RackConfig{
			"default": {Name: "default", URL: rack.URL, Username: "convox", APIKey: "rack-key", Enabled: true},
		},
	}
	auditLogger := audit.NewLogger(database)
	settingsService := settings.NewService(database)
	gateway := &deps.Gateway{
		Config:          cfg,
		Database:        database,
		RBACManager:     rbacManager,
		SessionManager:  sessions,
		AuthService:     auth.NewAuthService(tokenService, database, sessions),
		TokenService:    tokenService,
		MFAService:      mfaService,
		MFASettings:     &db.MFASettings{RequireAllUsers: true, StepUpWindowMinutes: 10, TrustedDeviceTTLDays: 30},
		SettingsService: settingsService,
		AuditLogger:     auditLogger,
		ProxyHandler: proxy.NewHandler(cfg, rbacManager, auditLogger, database, settingsService, nil,
			"default", "default", nil, mfaService, sessions),
	}
	router := gin.New()
	if err := router.SetTrustedProxies(nil); err != nil {
		t.Fatal(err)
	}
	routes.Setup(router, &routes.Config{Gateway: gateway})
	env.router = router
	return env
}

// enrollUser creates an admin user with a confirmed TOTP factor.
func (e *mfaRouteEnv) enrollUser(t *testing.T, email string) {
	t.Helper()
	user, err := e.database.CreateUser(email, "Test User", []string{"admin"})
	if err != nil {
		t.Fatal(err)
	}
	start, err := e.mfaService.StartTOTPEnrollment(user)
	if err != nil {
		t.Fatal(err)
	}
	// Use the previous TOTP step so codes generated "now" during the test aren't replays.
	code, err := totp.GenerateCode(start.Secret, time.Now().Add(-30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.mfaService.ConfirmTOTP(user, start.MethodID, code); err != nil {
		t.Fatal(err)
	}
	e.user, err = e.database.GetUser(email)
	if err != nil {
		t.Fatal(err)
	}
	e.totpSecret = start.Secret
}

type webSession struct {
	token   string
	session *db.UserSession
	headers map[string]string
}

// newSession creates a web session as the OAuth callback does (MFA not yet verified).
func (e *mfaRouteEnv) newSession(t *testing.T, user *db.User) *webSession {
	t.Helper()
	sessionToken, session, err := e.sessions.CreateSession(user, auth.SessionMetadata{Channel: "web"})
	if err != nil {
		t.Fatal(err)
	}
	csrf, err := e.sessions.DeriveCSRFToken(sessionToken)
	if err != nil {
		t.Fatal(err)
	}
	return &webSession{
		token:   sessionToken,
		session: session,
		headers: map[string]string{"Cookie": "session_token=" + sessionToken, "X-CSRF-Token": csrf},
	}
}

// markVerified marks the session's login MFA as complete. Without stepUp, the step-up
// timestamp is moved outside the step-up window.
func (e *mfaRouteEnv) markVerified(t *testing.T, s *webSession, stepUp bool) {
	t.Helper()
	now := time.Now()
	if err := e.database.UpdateSessionMFAVerified(s.session.ID, now, nil); err != nil {
		t.Fatal(err)
	}
	if !stepUp {
		if err := e.database.UpdateSessionRecentStepUp(s.session.ID, now.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
}

func (e *mfaRouteEnv) currentCode(t *testing.T) string {
	t.Helper()
	code, err := totp.GenerateCode(e.totpSecret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func (e *mfaRouteEnv) do(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Host = mfaTestHost
	req.RemoteAddr = "10.1.2.3:5555"
	req.Header.Set("User-Agent", "mfa-route-test")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	e.router.ServeHTTP(recorder, req)
	return recorder
}

func withHeader(headers map[string]string, key, value string) map[string]string {
	out := make(map[string]string, len(headers)+1)
	for k, v := range headers {
		out[k] = v
	}
	out[key] = value
	return out
}

func bearer(s *webSession) map[string]string {
	return map[string]string{"Authorization": "Bearer " + s.token}
}

func assertStatus(t *testing.T, w *httptest.ResponseRecorder, want int, label string) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("%s: expected %d, got %d: %s", label, want, w.Code, w.Body.String())
	}
}

func TestPendingMFASessionOnlyReachesChallengeRoutes(t *testing.T) {
	e := newMFARouteEnv(t, "pending@example.com")
	s := e.newSession(t, e.user)

	blocked := []struct{ method, path, body string }{
		{"GET", "/api/v1/users", ""},
		{"POST", "/api/v1/auth/mfa/enroll/totp/start", "{}"},
		{"POST", "/api/v1/api-tokens", `{"name":"x","permissions":["convox:app:list"]}`},
		{"POST", "/api/v1/auth/mfa/backup-codes/regenerate", "{}"},
	}
	for _, tc := range blocked {
		w := e.do(tc.method, tc.path, tc.body, s.headers)
		assertStatus(t, w, http.StatusUnauthorized, tc.method+" "+tc.path)
		if !strings.Contains(w.Body.String(), "mfa_step_up_required") {
			t.Fatalf("%s %s: expected mfa_step_up_required, got %s", tc.method, tc.path, w.Body.String())
		}
	}

	assertStatus(t, e.do("GET", "/api/v1/info", "", s.headers), http.StatusOK, "GET /api/v1/info")
	assertStatus(t, e.do("GET", "/api/v1/auth/mfa/status", "", s.headers), http.StatusOK, "GET mfa status")

	// The same session token used as a Bearer credential on the CLI proxy is refused too.
	w := e.do("GET", "/api/v1/rack-proxy/apps", "", bearer(s))
	assertStatus(t, w, http.StatusUnauthorized, "rack-proxy")
	if !strings.Contains(w.Body.String(), "must be completed for this session") {
		t.Fatalf("rack-proxy: expected pending-MFA denial, got %s", w.Body.String())
	}

	methods, err := e.database.ListMFAMethods(e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) != 1 {
		t.Fatalf("pending session must not be able to add MFA methods, have %d", len(methods))
	}
}

func TestPendingMFASessionUnlocksAfterVerification(t *testing.T) {
	e := newMFARouteEnv(t, "verify@example.com")
	s := e.newSession(t, e.user)

	body := fmt.Sprintf(`{"method":"totp","code":%q}`, e.currentCode(t))
	assertStatus(t, e.do("POST", "/api/v1/auth/mfa/verify", body, s.headers), http.StatusOK, "verify")

	assertStatus(t, e.do("GET", "/api/v1/users", "", s.headers), http.StatusOK, "GET users after verify")
	assertStatus(t, e.do("GET", "/api/v1/rack-proxy/apps", "", bearer(s)), http.StatusOK, "rack-proxy after verify")
}

func TestPendingMFASessionCanCompleteChallengeInline(t *testing.T) {
	e := newMFARouteEnv(t, "inline@example.com")
	s := e.newSession(t, e.user)

	headers := withHeader(s.headers, "X-MFA-TOTP", e.currentCode(t))
	assertStatus(t, e.do("GET", "/api/v1/users", "", headers), http.StatusOK, "GET users with inline TOTP")

	result, err := e.sessions.ValidateSession(s.token, "10.1.2.3", "mfa-route-test")
	if err != nil {
		t.Fatal(err)
	}
	if result.Session.MFAVerifiedAt == nil {
		t.Fatal("expected inline MFA to mark the session as verified")
	}
	assertStatus(t, e.do("GET", "/api/v1/users", "", s.headers), http.StatusOK, "GET users afterwards")
}

func TestAddingFactorRequiresRecentStepUp(t *testing.T) {
	e := newMFARouteEnv(t, "stepup@example.com")
	s := e.newSession(t, e.user)
	e.markVerified(t, s, false)

	w := e.do("POST", "/api/v1/auth/mfa/enroll/totp/start", "{}", s.headers)
	assertStatus(t, w, http.StatusUnauthorized, "enroll without step-up")
	if !strings.Contains(w.Body.String(), "mfa_step_up_required") {
		t.Fatalf("expected mfa_step_up_required, got %s", w.Body.String())
	}

	if err := e.database.UpdateSessionRecentStepUp(s.session.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	w = e.do("POST", "/api/v1/auth/mfa/enroll/totp/start", "{}", s.headers)
	assertStatus(t, w, http.StatusOK, "enroll with step-up")

	var resp struct {
		BackupCodes []string `json:"backup_codes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.BackupCodes) != 0 {
		t.Fatalf("adding a second factor must not reissue backup codes, got %d", len(resp.BackupCodes))
	}
}

func TestFirstEnrollmentNeedsNoStepUp(t *testing.T) {
	e := newMFARouteEnv(t, "")
	user, err := e.database.CreateUser("new@example.com", "New User", []string{"admin"})
	if err != nil {
		t.Fatal(err)
	}
	s := e.newSession(t, user)

	w := e.do("POST", "/api/v1/auth/mfa/enroll/totp/start", "{}", s.headers)
	assertStatus(t, w, http.StatusOK, "first enrollment start")
	var start struct {
		MethodID    int64    `json:"method_id"`
		Secret      string   `json:"secret"`
		BackupCodes []string `json:"backup_codes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}
	if len(start.BackupCodes) == 0 {
		t.Fatal("expected backup codes on first enrollment")
	}

	code, err := totp.GenerateCode(start.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"method_id":%d,"code":%q}`, start.MethodID, code)
	assertStatus(t, e.do("POST", "/api/v1/auth/mfa/enroll/totp/confirm", body, s.headers), http.StatusOK,
		"first enrollment confirm")

	enrolled, err := e.database.GetUser(user.Email)
	if err != nil {
		t.Fatal(err)
	}
	if !enrolled.MFAEnrolled {
		t.Fatal("expected user to be enrolled")
	}
}

func TestMFAChangesAreAudited(t *testing.T) {
	e := newMFARouteEnv(t, "audited@example.com")
	s := e.newSession(t, e.user)
	e.markVerified(t, s, true)

	assertStatus(t, e.do("POST", "/api/v1/auth/mfa/backup-codes/regenerate", "{}", s.headers), http.StatusOK,
		"regenerate backup codes")

	methods, err := e.database.ListMFAMethods(e.user.ID)
	if err != nil || len(methods) == 0 {
		t.Fatalf("expected an MFA method, err=%v", err)
	}
	deletePath := fmt.Sprintf("/api/v1/auth/mfa/methods/%d", methods[0].ID)
	headers := withHeader(s.headers, "X-MFA-TOTP", e.currentCode(t))
	assertStatus(t, e.do("DELETE", deletePath, "", headers), http.StatusOK, "delete MFA method")

	for _, action := range []string{"mfa_backup_codes.generate", "mfa_method.delete"} {
		var count int
		row := e.database.DB().QueryRow(`SELECT COUNT(*) FROM audit.audit_event WHERE action = $1`, action)
		if err := row.Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("expected one %s audit event, got %d", action, count)
		}
	}
}
