package envutil

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

const redactedDetails = `{"old":"[REDACTED]","new":"[REDACTED]"}`

// Every value is redacted unless the app lists its key in audit_visible_env_vars.
func TestAuditDetailsRedactsUnlistedKeys(t *testing.T) {
	none := AuditVisibleKeys{}
	for _, key := range []string{"DATABASE_PASSWORD", "DATABASE_URL", "PORT"} {
		require.Equal(t, redactedDetails, none.AuditDetails(EnvDiff{Key: key, OldVal: "a", NewVal: "b"}), key)
	}

	visible := newAuditVisibleKeys([]string{" port "}, nil)
	port := EnvDiff{Key: "PORT", OldVal: "3000", NewVal: "4000"}
	require.Equal(t, `{"old":"3000","new":"4000"}`, visible.AuditDetails(port))
	require.Equal(t, `{"old":"3000","new":""}`, visible.AuditDetails(EnvDiff{Key: "Port", OldVal: "3000"}))
	require.Equal(t, redactedDetails, visible.AuditDetails(EnvDiff{Key: "DATABASE_PASSWORD", OldVal: "a", NewVal: "b"}))
}

// Listing a key never shows a secret or protected value.
func TestAuditDetailsNeverShowsSecretOrProtectedValues(t *testing.T) {
	visible := newAuditVisibleKeys(
		[]string{"PORT", "STRIPE_SECRET", "SIGNING_KEY", "BILLING_ID", "ADMIN_PASSWORD"},
		[]string{"billing_id", "ADMIN_PASSWORD"},
	)
	require.Equal(t, AuditVisibleKeys{"PORT": {}}, visible)

	for _, d := range []EnvDiff{
		{Key: "STRIPE_SECRET", OldVal: "a", NewVal: "b"},
		{Key: "SIGNING_KEY", OldVal: "a", NewVal: "b"},
		{Key: "BILLING_ID", OldVal: "a", NewVal: "b"},
		{Key: "ADMIN_PASSWORD", OldVal: "a", NewVal: "b"},
		{Key: "PORT", OldVal: "a", NewVal: "b", Secret: true},
		{Key: "PORT", OldVal: "a", NewVal: "b", Protected: true},
	} {
		require.Equal(t, redactedDetails, visible.AuditDetails(d), "%+v", d)
	}
}

// Shown values are JSON-encoded, whatever characters they hold.
func TestAuditDetailsEncodesValues(t *testing.T) {
	visible := newAuditVisibleKeys([]string{"NOTE"}, nil)
	value := "say \"hi\"\\\t\x01<b>"
	var details map[string]string
	require.NoError(t, json.Unmarshal([]byte(visible.AuditDetails(EnvDiff{Key: "NOTE", NewVal: value})), &details))
	require.Equal(t, map[string]string{"old": "", "new": value}, details)
}

type fakeAuditSettings struct {
	visible, secret, protected []string
	err                        error
	failOn                     string
}

func (f fakeAuditSettings) list(name string, values []string) ([]string, error) {
	if f.failOn == name {
		return nil, f.err
	}
	return values, nil
}

func (f fakeAuditSettings) GetAuditVisibleEnvVars(string) ([]string, error) {
	return f.list("visible", f.visible)
}

func (f fakeAuditSettings) GetSecretEnvVars(string) ([]string, error) {
	return f.list("secret", f.secret)
}

func (f fakeAuditSettings) GetProtectedEnvVars(string) ([]string, error) {
	return f.list("protected", f.protected)
}

// The app's secret and protected env vars and CONVOX_SECRET_ENV_VARS stay hidden, and a settings error hides
// everything.
func TestLoadAuditVisibleKeys(t *testing.T) {
	s := fakeAuditSettings{
		visible:   []string{"PORT", "RAILS_ENV", "BILLING_ID", "DATABASE_URL", "ADMIN_EMAIL"},
		secret:    []string{"BILLING_ID"},
		protected: []string{"ADMIN_EMAIL"},
		err:       errors.New("database unavailable"),
	}
	keys, err := LoadAuditVisibleKeys(s, "web", []string{"DATABASE_URL"})
	require.NoError(t, err)
	require.Equal(t, AuditVisibleKeys{"PORT": {}, "RAILS_ENV": {}}, keys)

	for _, failOn := range []string{"visible", "secret", "protected"} {
		s.failOn = failOn
		keys, err = LoadAuditVisibleKeys(s, "web", nil)
		require.ErrorIs(t, err, s.err, failOn)
		require.Empty(t, keys, failOn)
	}
}
