package app

import (
	"strings"
	"testing"

	"github.com/DocSpring/rack-gateway/internal/gateway/colcrypt"
)

// randomBase64 stands in for `openssl rand -base64 32` output: 32 bytes (0 to 31) in base64, 44 characters.
const randomBase64 = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="

// fakeEnv returns a getenv that reads values, and "" for any other name.
func fakeEnv(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

// testbypass.TestActive covers which settings count as bypasses.
func TestCheckProductionSafety(t *testing.T) {
	bypasses := map[string]string{"DEV_MODE": "true", "E2E_TEST_MODE": "true", "AWS_ENDPOINT_URL_S3": "x"}

	if err := checkProductionSafety("production", false, fakeEnv(nil)); err != nil {
		t.Errorf("production without bypasses: err=%v, want nil", err)
	}
	if err := checkProductionSafety("development", false, fakeEnv(bypasses)); err != nil {
		t.Errorf("development with bypasses: err=%v, want nil", err)
	}
	err := checkProductionSafety("production", false, fakeEnv(bypasses))
	if err == nil || !strings.Contains(err.Error(), "DEV_MODE, E2E_TEST_MODE, AWS_ENDPOINT_URL_S3") {
		t.Errorf("production with bypasses: err=%v, want one naming each bypass", err)
	}
}

// The development column key is published in this repository, so a production database refuses it.
func TestCheckProductionSafetyRefusesTheDevelopmentColumnKey(t *testing.T) {
	for _, key := range []string{colcrypt.DevKey, " " + colcrypt.DevKey + "\n"} {
		err := checkProductionSafety("production", false, fakeEnv(map[string]string{colcrypt.EnvVar: key}))
		if err == nil || !strings.Contains(err.Error(), colcrypt.EnvVar) {
			t.Errorf("production with the development key %q: err=%v, want a refusal naming it", key, err)
		}
	}
	ownKey := fakeEnv(map[string]string{colcrypt.EnvVar: randomBase64})
	if err := checkProductionSafety("production", false, ownKey); err != nil {
		t.Errorf("production with its own key: err=%v, want nil", err)
	}
	devKey := fakeEnv(map[string]string{colcrypt.EnvVar: colcrypt.DevKey})
	if err := checkProductionSafety("development", false, devKey); err != nil {
		t.Errorf("development with the development key: err=%v, want nil", err)
	}
}

// APP_SECRET_KEY and AUDIT_HMAC_SECRET can't be the development values published in this repository (mise.toml,
// docker-compose.yml, the audit writer's fallback) or shorter than 32 bytes, in a release build or against a
// production database. A testbypass build against a development database (local dev, CI, E2E) still accepts them.
func TestCheckProductionSafetyRefusesWeakSecrets(t *testing.T) {
	weak := map[string]map[string]string{
		"APP_SECRET_KEY (the development value published in this repository)": {
			"APP_SECRET_KEY": developmentAppKey,
		},
		"AUDIT_HMAC_SECRET (the development value published in this repository)": {
			"AUDIT_HMAC_SECRET": " development-audit-hmac-secret-change-in-production\n",
		},
		"APP_SECRET_KEY (shorter than 32 bytes)":    {"APP_SECRET_KEY": strings.Repeat("k", minSecretBytes-1)},
		"AUDIT_HMAC_SECRET (shorter than 32 bytes)": {"AUDIT_HMAC_SECRET": "release-smoke-audit-secret"},
	}
	for want, values := range weak {
		for _, mode := range []struct {
			dbEnvironment string
			releaseBuild  bool
		}{{"development", true}, {"production", true}, {"production", false}} {
			err := checkProductionSafety(mode.dbEnvironment, mode.releaseBuild, fakeEnv(values))
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%+v with %v: err=%v, want a refusal naming %q", mode, values, err, want)
			}
		}
		if err := checkProductionSafety("development", false, fakeEnv(values)); err != nil {
			t.Errorf("testbypass build, development database, %v: err=%v, want nil", values, err)
		}
	}

	strong := map[string]string{"APP_SECRET_KEY": randomBase64, "AUDIT_HMAC_SECRET": randomBase64}
	if err := checkProductionSafety("production", true, fakeEnv(strong)); err != nil {
		t.Errorf("release build with strong secrets: err=%v, want nil", err)
	}
	if err := checkProductionSafety("production", true, fakeEnv(nil)); err != nil {
		t.Errorf("release build with the secrets unset (refused elsewhere): err=%v, want nil", err)
	}
}
