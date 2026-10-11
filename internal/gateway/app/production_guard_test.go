package app

import (
	"strings"
	"testing"

	"github.com/DocSpring/rack-gateway/internal/gateway/colcrypt"
)

// testbypass.TestActive covers which settings count as bypasses.
func TestCheckProductionSafety(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(name string) string { return values[name] }
	}
	bypasses := map[string]string{"DEV_MODE": "true", "E2E_TEST_MODE": "true", "AWS_ENDPOINT_URL_S3": "x"}

	if err := checkProductionSafety("production", env(nil)); err != nil {
		t.Errorf("production without bypasses: err=%v, want nil", err)
	}
	if err := checkProductionSafety("development", env(bypasses)); err != nil {
		t.Errorf("development with bypasses: err=%v, want nil", err)
	}
	err := checkProductionSafety("production", env(bypasses))
	if err == nil || !strings.Contains(err.Error(), "DEV_MODE, E2E_TEST_MODE, AWS_ENDPOINT_URL_S3") {
		t.Errorf("production with bypasses: err=%v, want one naming each bypass", err)
	}
}

// The development column key is published in this repository, so a production database refuses it.
func TestCheckProductionSafetyRefusesTheDevelopmentColumnKey(t *testing.T) {
	env := func(key string) func(string) string {
		return func(name string) string {
			if name == colcrypt.EnvVar {
				return key
			}
			return ""
		}
	}
	for _, key := range []string{colcrypt.DevKey, " " + colcrypt.DevKey + "\n"} {
		err := checkProductionSafety("production", env(key))
		if err == nil || !strings.Contains(err.Error(), colcrypt.EnvVar) {
			t.Errorf("production with the development key %q: err=%v, want a refusal naming it", key, err)
		}
	}
	if err := checkProductionSafety("production", env("AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=")); err != nil {
		t.Errorf("production with its own key: err=%v, want nil", err)
	}
	if err := checkProductionSafety("development", env(colcrypt.DevKey)); err != nil {
		t.Errorf("development with the development key: err=%v, want nil", err)
	}
}
