package middleware

import "testing"

func TestShouldFilterHTTPLog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "api request with email",
			path: "/api/v1/users/e2e-edit-123%40example.com",
			want: false,
		},
		{
			name: "node modules asset",
			path: "/app/node_modules/some-lib/index.js",
			want: true,
		},
		{
			name: "static asset extension",
			path: "/assets/app.css",
			want: true,
		},
		{
			name: "non asset with dot but not api",
			path: "/app/items/version-1.2",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldFilterHTTPLog(tt.path); got != tt.want {
				t.Fatalf("shouldFilterHTTPLog(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
