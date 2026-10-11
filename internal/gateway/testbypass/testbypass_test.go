package testbypass

import (
	"reflect"
	"testing"
)

func fakeEnv(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestActive(t *testing.T) {
	cases := []struct {
		name string
		vars map[string]string
		want []string
	}{
		{"nothing set", nil, nil},
		{
			"flags explicitly off",
			map[string]string{"DEV_MODE": "false", "E2E_TEST_MODE": "0", "COOKIE_SECURE": "true"},
			nil,
		},
		{"unparseable flag", map[string]string{"DEV_MODE": "yes"}, nil},
		{"https OIDC issuer", map[string]string{"GOOGLE_OAUTH_BASE_URL": "https://accounts.example.com"}, nil},
		{"DEV_MODE", map[string]string{"DEV_MODE": "1"}, []string{"DEV_MODE"}},
		{"E2E_TEST_MODE", map[string]string{"E2E_TEST_MODE": " true "}, []string{"E2E_TEST_MODE"}},
		{"insecure cookies", map[string]string{"COOKIE_SECURE": "false"}, []string{"COOKIE_SECURE=false"}},
		{"S3 endpoint", map[string]string{"AWS_ENDPOINT_URL_S3": "http://minio:9000"}, []string{"AWS_ENDPOINT_URL_S3"}},
		{"global AWS endpoint", map[string]string{"AWS_ENDPOINT_URL": "http://x:4566"}, []string{"AWS_ENDPOINT_URL"}},
		{"STS endpoint", map[string]string{"AWS_ENDPOINT_URL_STS": "http://x"}, []string{"AWS_ENDPOINT_URL_STS"}},
		{"KMS endpoint", map[string]string{"AWS_ENDPOINT_URL_KMS": "http://x"}, []string{"AWS_ENDPOINT_URL_KMS"}},
		{"Postmark endpoint", map[string]string{"POSTMARK_API_BASE": "http://evil"}, []string{"POSTMARK_API_BASE"}},
		{
			"http OIDC issuer",
			map[string]string{"GOOGLE_OAUTH_BASE_URL": "http://mock-oauth:3345"},
			[]string{"GOOGLE_OAUTH_BASE_URL (must be https)"},
		},
		{
			"several, in a fixed order",
			map[string]string{
				"GOOGLE_OAUTH_BASE_URL": "http://mock-oauth:3345",
				"AWS_ENDPOINT_URL_S3":   "http://minio:9000",
				"E2E_TEST_MODE":         "true",
				"DEV_MODE":              "true",
			},
			[]string{"DEV_MODE", "E2E_TEST_MODE", "AWS_ENDPOINT_URL_S3", "GOOGLE_OAUTH_BASE_URL (must be https)"},
		},
	}
	for _, tc := range cases {
		if got := Active(fakeEnv(tc.vars)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: Active() = %q, want %q", tc.name, got, tc.want)
		}
	}
}
