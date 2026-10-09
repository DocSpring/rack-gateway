package app

import "testing"

func TestCheckProductionSafety(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(name string) string { return values[name] }
	}
	cases := []struct {
		name    string
		dbEnv   string
		vars    map[string]string
		wantErr bool
	}{
		{"production with nothing set", "production", nil, false},
		{
			"production with flags explicitly false", "production",
			map[string]string{"DEV_MODE": "false", "E2E_TEST_MODE": "0"},
			false,
		},
		{
			"production with https OIDC issuer", "production",
			map[string]string{"GOOGLE_OAUTH_BASE_URL": "https://accounts.example.com"},
			false,
		},
		{"production with E2E_TEST_MODE", "production", map[string]string{"E2E_TEST_MODE": "true"}, true},
		{"production with DEV_MODE", "production", map[string]string{"DEV_MODE": "1"}, true},
		{
			"production with S3 endpoint override", "production",
			map[string]string{"AWS_ENDPOINT_URL_S3": "http://minio:9000"},
			true,
		},
		{
			"production with global AWS endpoint override", "production",
			map[string]string{"AWS_ENDPOINT_URL": "http://localstack:4566"},
			true,
		},
		{
			"production with STS endpoint override", "production",
			map[string]string{"AWS_ENDPOINT_URL_STS": "http://localstack:4566"},
			true,
		},
		{
			"production with Postmark override", "production",
			map[string]string{"POSTMARK_API_BASE": "http://evil"},
			true,
		},
		{
			"production with http OIDC issuer", "production",
			map[string]string{"GOOGLE_OAUTH_BASE_URL": "http://mock-oauth:3345"},
			true,
		},
		{
			"development allows test switches", "development",
			map[string]string{"DEV_MODE": "true", "E2E_TEST_MODE": "true", "AWS_ENDPOINT_URL_S3": "x"},
			false,
		},
	}
	for _, tc := range cases {
		err := checkProductionSafety(tc.dbEnv, env(tc.vars))
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err=%v, wantErr=%v", tc.name, err, tc.wantErr)
		}
	}
}
