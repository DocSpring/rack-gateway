package app

import (
	"strings"
	"testing"
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
