package rbac

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeyMatch3Multi(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		pattern  string
		expected bool
	}{
		// Single-segment patterns {var}
		{
			name:     "single segment matches exactly",
			path:     "/apps/myapp/processes/p1",
			pattern:  "/apps/{app}/processes/{pid}",
			expected: true,
		},
		{
			name:     "single segment does NOT match with extra path",
			path:     "/apps/myapp/processes/p1/exec",
			pattern:  "/apps/{app}/processes/{pid}",
			expected: false,
		},
		{
			name:     "single segment does NOT match with trailing slash",
			path:     "/apps/myapp/processes/p1/",
			pattern:  "/apps/{app}/processes/{pid}",
			expected: false,
		},

		// Multi-segment patterns {var:.*}
		{
			name:     "multi-segment matches single segment",
			path:     "/apps/myapp/objects/file.txt",
			pattern:  "/apps/{app}/objects/{key:.*}",
			expected: true,
		},
		{
			name:     "multi-segment matches nested path",
			path:     "/apps/myapp/objects/path/to/file.txt",
			pattern:  "/apps/{app}/objects/{key:.*}",
			expected: true,
		},
		{
			name:     "multi-segment matches deeply nested path",
			path:     "/apps/myapp/objects/very/deep/path/to/file.txt",
			pattern:  "/apps/{app}/objects/{key:.*}",
			expected: true,
		},
		{
			name:     "multi-segment requires at least one segment",
			path:     "/apps/myapp/objects/",
			pattern:  "/apps/{app}/objects/{key:.*}",
			expected: false,
		},
		{
			name:     "multi-segment without suffix",
			path:     "/apps/myapp/objects",
			pattern:  "/apps/{app}/objects/{key:.*}",
			expected: false,
		},

		// Registry paths
		{
			name:     "registry multi-segment matches",
			path:     "/v2/library/nginx/manifests/latest",
			pattern:  "/v2/{path:.*}",
			expected: true,
		},
		{
			name:     "custom proxy multi-segment matches",
			path:     "/custom/http/proxy/path/to/service",
			pattern:  "/custom/http/proxy/{path:.*}",
			expected: true,
		},

		// Wildcard * patterns
		{
			name:     "wildcard matches within segment",
			path:     "/apps/my-app-123",
			pattern:  "/apps/*",
			expected: true,
		},
		{
			name:     "wildcard does NOT match across segments",
			path:     "/apps/myapp/extra",
			pattern:  "/apps/*",
			expected: false,
		},
		{
			name:     "wildcard matches empty",
			path:     "/apps/",
			pattern:  "/apps/*",
			expected: true,
		},

		// Mixed patterns
		{
			name:     "mix of single and multi segment",
			path:     "/apps/myapp/resources/db/data/backup.sql",
			pattern:  "/apps/{app}/resources/{name}/data/{file:.*}",
			expected: true,
		},

		// Negative cases - wrong prefix
		{
			name:     "wrong prefix fails",
			path:     "/appsX/myapp/objects/file.txt",
			pattern:  "/apps/{app}/objects/{key:.*}",
			expected: false,
		},
		{
			name:     "objectsX doesn't match objects",
			path:     "/apps/myapp/objectsX/path",
			pattern:  "/apps/{app}/objects/{key:.*}",
			expected: false,
		},

		// URL-encoded keys
		{
			name:     "URL-encoded slash in filename",
			path:     "/apps/myapp/objects/a%2Fb.txt",
			pattern:  "/apps/{app}/objects/{key:.*}",
			expected: true,
		},

		// Edge cases
		{
			name:     "empty app name",
			path:     "/apps//objects/file.txt",
			pattern:  "/apps/{app}/objects/{key:.*}",
			expected: false,
		},
		{
			name:     "exact match without patterns",
			path:     "/health",
			pattern:  "/health",
			expected: true,
		},
		{
			name:     "exact match with trailing difference",
			path:     "/health/check",
			pattern:  "/health",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := keyMatch3Multi(tt.path, tt.pattern)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result, "Failed for path=%s pattern=%s", tt.path, tt.pattern)
		})
	}
}
