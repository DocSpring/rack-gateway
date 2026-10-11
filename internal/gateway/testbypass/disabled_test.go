//go:build !testbypass

package testbypass

import (
	"strings"
	"testing"
)

func TestReleaseBuildIgnoresBypasses(t *testing.T) {
	t.Setenv("DEV_MODE", "true")
	t.Setenv("E2E_TEST_MODE", "true")
	t.Setenv("COOKIE_SECURE", "false")
	t.Setenv("AWS_ENDPOINT_URL_S3", "http://minio:9000")
	t.Setenv("POSTMARK_API_BASE", "http://mock-postmark")

	if Compiled {
		t.Error("Compiled = true in a build without the testbypass tag")
	}
	if DevMode() || E2ETestMode() || InsecureCookies(true) || S3EndpointOverride() || PostmarkAPIBase() != "" {
		t.Errorf("DevMode=%t E2ETestMode=%t InsecureCookies=%t S3EndpointOverride=%t PostmarkAPIBase=%q, want all off",
			DevMode(), E2ETestMode(), InsecureCookies(true), S3EndpointOverride(), PostmarkAPIBase())
	}
}

func TestReleaseBuildRefusesToStartWithBypasses(t *testing.T) {
	err := Check(fakeEnv(map[string]string{
		"DEV_MODE":            "true",
		"E2E_TEST_MODE":       "1",
		"AWS_ENDPOINT_URL_S3": "http://minio:9000",
	}))
	if err == nil {
		t.Fatal("Check() = nil, want an error naming the bypasses")
	}
	for _, want := range []string{"doesn't support test bypasses", "DEV_MODE, E2E_TEST_MODE, AWS_ENDPOINT_URL_S3"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Check() = %q, want it to contain %q", err, want)
		}
	}
}

func TestReleaseBuildStartsWithoutBypasses(t *testing.T) {
	err := Check(fakeEnv(map[string]string{
		"DEV_MODE":              "false",
		"COOKIE_SECURE":         "true",
		"GOOGLE_OAUTH_BASE_URL": "https://accounts.google.com",
	}))
	if err != nil {
		t.Errorf("Check() = %v, want nil", err)
	}
}
