package proxy

import "testing"

func TestValidateServiceImages(t *testing.T) {
	tests := []struct {
		name            string
		manifest        *convoxManifest
		servicePatterns map[string]string
		wantErr         bool
		errContains     string
	}{
		{
			name: "no patterns - all services pass",
			manifest: &convoxManifest{
				Services: map[string]convoxService{
					"web":    {Image: "myapp:latest"},
					"worker": {Image: "myapp:latest"},
				},
			},
			servicePatterns: map[string]string{},
			wantErr:         false,
		},
		{
			name: "specific service pattern - exact match passes",
			manifest: &convoxManifest{
				Services: map[string]convoxService{
					"web":    {Image: "docker.io/myapp:abc123-amd64"},
					"worker": {Image: "anything:latest"},
				},
			},
			servicePatterns: map[string]string{
				"web": `^docker\.io/myapp:[a-f0-9]+-amd64$`,
			},
			wantErr: false,
		},
		{
			name: "specific service pattern - mismatch fails",
			manifest: &convoxManifest{
				Services: map[string]convoxService{
					"web": {Image: "docker.io/myapp:latest"},
				},
			},
			servicePatterns: map[string]string{
				"web": `^docker\.io/myapp:[a-f0-9]+-amd64$`,
			},
			wantErr:     true,
			errContains: "does not match required pattern",
		},
		{
			name: "wildcard pattern - applies to all services without specific pattern",
			manifest: &convoxManifest{
				Services: map[string]convoxService{
					"web":    {Image: "docker.io/myapp:abc123-amd64"},
					"worker": {Image: "docker.io/myapp:def456-amd64"},
				},
			},
			servicePatterns: map[string]string{
				"*": `^docker\.io/myapp:[a-f0-9]+-amd64$`,
			},
			wantErr: false,
		},
		{
			name: "wildcard pattern - one service fails",
			manifest: &convoxManifest{
				Services: map[string]convoxService{
					"web":    {Image: "docker.io/myapp:abc123-amd64"},
					"worker": {Image: "docker.io/myapp:latest"},
				},
			},
			servicePatterns: map[string]string{
				"*": `^docker\.io/myapp:[a-f0-9]+-amd64$`,
			},
			wantErr:     true,
			errContains: "worker",
		},
		{
			name: "specific pattern overrides wildcard",
			manifest: &convoxManifest{
				Services: map[string]convoxService{
					"web":    {Image: "docker.io/myapp:abc123-amd64"},
					"worker": {Image: "docker.io/myapp:def456-amd64"},
					"admin":  {Image: "docker.io/admin:xyz789"},
				},
			},
			servicePatterns: map[string]string{
				"*":     `^docker\.io/myapp:[a-f0-9]+-amd64$`,
				"admin": `^docker\.io/admin:.*$`,
			},
			wantErr: false,
		},
		{
			name: "pattern requires image, but build is used",
			manifest: &convoxManifest{
				Services: map[string]convoxService{
					"web": {Image: "", Build: "."},
				},
			},
			servicePatterns: map[string]string{
				"web": `^docker\.io/myapp:.*$`,
			},
			wantErr:     true,
			errContains: "must use a pre-built image",
		},
		{
			name: "wildcard pattern requires image, but build is used",
			manifest: &convoxManifest{
				Services: map[string]convoxService{
					"web": {Image: "", Build: "."},
				},
			},
			servicePatterns: map[string]string{
				"*": `^docker\.io/myapp:.*$`,
			},
			wantErr:     true,
			errContains: "must use a pre-built image",
		},
		{
			name: "invalid regex pattern",
			manifest: &convoxManifest{
				Services: map[string]convoxService{
					"web": {Image: "myapp:latest"},
				},
			},
			servicePatterns: map[string]string{
				"web": `[invalid(regex`,
			},
			wantErr:     true,
			errContains: "invalid image pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateServiceImages(tt.manifest, tt.servicePatterns)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateServiceImages() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errContains != "" {
				if !contains(tt.errContains, err.Error()) {
					t.Errorf(
						"validateServiceImages() error = %v, want error containing %q",
						err,
						tt.errContains,
					)
				}
			}
		})
	}
}

func TestBuildObjectFetchURL(t *testing.T) {
	t.Parallel()

	t.Run("builds fetch URL within rack namespace", func(t *testing.T) {
		t.Parallel()

		fetchURL, err := buildObjectFetchURL("https://rack.example.com/api", "docspring", "tmp/file.tgz")
		if err != nil {
			t.Fatalf("buildObjectFetchURL returned error: %v", err)
		}

		const want = "https://rack.example.com/api/apps/docspring/objects/tmp/file.tgz"
		if fetchURL != want {
			t.Fatalf("buildObjectFetchURL returned %q, want %q", fetchURL, want)
		}
	})

	t.Run("rejects traversal in object key", func(t *testing.T) {
		t.Parallel()

		if _, err := buildObjectFetchURL("https://rack.example.com", "docspring", "../secrets.tgz"); err == nil {
			t.Fatal("expected traversal key to be rejected")
		}
	})

	t.Run("rejects invalid app path", func(t *testing.T) {
		t.Parallel()

		if _, err := buildObjectFetchURL("https://rack.example.com", "docspring/api", "tmp/file.tgz"); err == nil {
			t.Fatal("expected invalid app path to be rejected")
		}
	})
}

func contains(substr, s string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && anySubstring(s, substr)))
}

func anySubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
