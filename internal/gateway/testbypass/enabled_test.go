//go:build testbypass

package testbypass

import "testing"

func TestEnabledBuildHonoursBypasses(t *testing.T) {
	t.Setenv("DEV_MODE", "true")
	t.Setenv("E2E_TEST_MODE", "true")
	t.Setenv("AWS_ENDPOINT_URL_S3", "http://minio:9000")
	t.Setenv("POSTMARK_API_BASE", "http://mock-postmark")

	if !Compiled {
		t.Error("Compiled = false in a testbypass build")
	}
	if !DevMode() || !E2ETestMode() || !S3EndpointOverride() {
		t.Errorf("DevMode=%t E2ETestMode=%t S3EndpointOverride=%t, want all true",
			DevMode(), E2ETestMode(), S3EndpointOverride())
	}
	if got := PostmarkAPIBase(); got != "http://mock-postmark" {
		t.Errorf("PostmarkAPIBase() = %q", got)
	}
	if err := Check(fakeEnv(map[string]string{"DEV_MODE": "true"})); err != nil {
		t.Errorf("Check() = %v, want nil", err)
	}
}

func TestEnabledBuildReadsFlagsOff(t *testing.T) {
	t.Setenv("DEV_MODE", "false")
	t.Setenv("E2E_TEST_MODE", "")
	t.Setenv("AWS_ENDPOINT_URL_S3", "")
	t.Setenv("POSTMARK_API_BASE", "")

	if DevMode() || E2ETestMode() || S3EndpointOverride() || PostmarkAPIBase() != "" {
		t.Errorf("DevMode=%t E2ETestMode=%t S3EndpointOverride=%t PostmarkAPIBase=%q, want all off",
			DevMode(), E2ETestMode(), S3EndpointOverride(), PostmarkAPIBase())
	}
}

func TestEnabledBuildInsecureCookies(t *testing.T) {
	cases := []struct {
		cookieSecure string
		devMode      bool
		want         bool
	}{
		{"", false, false},
		{"", true, true},
		{"true", true, false},
		{"false", false, true},
		{"not-a-bool", true, true},
	}
	for _, tc := range cases {
		t.Setenv("COOKIE_SECURE", tc.cookieSecure)
		if got := InsecureCookies(tc.devMode); got != tc.want {
			t.Errorf("COOKIE_SECURE=%q devMode=%t: InsecureCookies() = %t, want %t",
				tc.cookieSecure, tc.devMode, got, tc.want)
		}
	}
}
