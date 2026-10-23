package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
)

// tlsRack is a rack served over TLS with a self-signed certificate. It has one app, "myapp", and records the
// paths it's asked for.
type tlsRack struct {
	mu    sync.Mutex
	paths []string
}

func (rk *tlsRack) requested() []string {
	rk.mu.Lock()
	defer rk.mu.Unlock()
	return append([]string(nil), rk.paths...)
}

// useTLSRack points the fixture at a TLS rack that the gateway reaches with its pinned certificate.
func useTLSRack(t *testing.T, f *createApprovalFixture) *tlsRack {
	t.Helper()
	rk := &tlsRack{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rk.mu.Lock()
		rk.paths = append(rk.paths, r.URL.RequestURI())
		rk.mu.Unlock()
		if r.URL.RequestURI() == "/apps/myapp" {
			_, _ = w.Write([]byte(`{"name":"myapp"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	f.handler.config.Racks["default"] = config.RackConfig{
		Name: "staging", Enabled: true, URL: server.URL, Username: "convox", APIKey: "rack-key",
	}
	f.handler.rackCertManager = rackcert.NewManager(f.handler.config, f.handler.database)
	return rk
}

// The app is checked on the rack over the rack's pinned certificate, as the proxy reaches the rack, so a rack with
// a self-signed certificate works.
func TestCreateChecksTheAppOnTheRack(t *testing.T) {
	f := newCreateApprovalFixture(t)
	rack := useTLSRack(t, f)

	resp := f.createWith(t, map[string]string{"app": "other"})
	require.Equal(t, http.StatusNotFound, resp.StatusCode, responseError(t, resp))
	requireNoRequestCreated(t, f)

	resp = f.createWith(t, nil)
	require.Equal(t, http.StatusCreated, resp.StatusCode, responseError(t, resp))
	require.Equal(t, []string{"/apps/other", "/apps/myapp"}, rack.requested())
}

// An app name goes into the rack URL, so only a Convox app name is accepted. Anything else could ask the rack about
// another app ("myapp?x", "myapp/../x") and pass the check.
func TestCreateRefusesInvalidAppNames(t *testing.T) {
	f := newCreateApprovalFixture(t)
	rack := useTLSRack(t, f)

	for _, app := range []string{
		"myapp?x", "myapp/../x", "myapp#x", "my%61pp", "MyApp", "my_app", "-myapp", "myapp-",
		"system", "rack", strings.Repeat("a", 64),
	} {
		resp := f.createWith(t, map[string]string{"app": app})
		require.Equalf(t, http.StatusBadRequest, resp.StatusCode, "app %q: %s", app, responseError(t, resp))
	}
	requireNoRequestCreated(t, f)
	require.Empty(t, rack.requested(), "an invalid app name never reaches the rack")
}

// A message is at most 2000 characters.
func TestCreateRefusesAnOverlongMessage(t *testing.T) {
	f := newCreateApprovalFixture(t)
	resp := f.createWith(t, map[string]string{"message": strings.Repeat("é", 2001)})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, responseError(t, resp), "at most 2000 characters")
	requireNoRequestCreated(t, f)

	resp = f.createWith(t, map[string]string{"message": strings.Repeat("é", 2000)})
	require.Equal(t, http.StatusCreated, resp.StatusCode, responseError(t, resp))
}

// The create audit entry keeps every detail of the request and its GitHub verification, whatever the message. A
// message that would fill the entry once JSON-escaped ("<" takes six characters) is stored without the escaping.
func TestCreateAuditEntryKeepsVerificationDetailsWithALongMessage(t *testing.T) {
	f := newVerifyingFixture(t, true, nil)
	message := strings.Repeat("<", maxDeployApprovalMessageChars)
	resp := f.createWith(t, map[string]string{"message": message, "git_commit_hash": ghBranchHead})
	require.Equal(t, http.StatusCreated, resp.StatusCode, responseError(t, resp))

	details := createAuditDetails(t, f)
	for key, want := range map[string]string{
		"token_uuid":             f.token.PublicID,
		"git_commit_hash":        ghBranchHead,
		"git_branch":             "feature/deploy",
		"github_verification":    "passed",
		"vcs_repo":               "acme/web",
		"verify_git_commit_mode": "latest",
		"require_pr_for_branch":  "true",
		"pr_url":                 ghPRURL,
	} {
		require.Equal(t, want, details[key], key)
	}
	require.Equal(t, message, details["message"])
}

// Without a configured window, an approval stays open for the documented default of 15 minutes.
func TestDeployApprovalWindowDefaultsTo15Minutes(t *testing.T) {
	expiresAt, err := (&AdminHandler{}).getDeployApprovalExpiry()
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(15*time.Minute), expiresAt, time.Minute)
}
