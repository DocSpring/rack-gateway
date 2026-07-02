package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Release promotes run for minutes on the rack API and cannot be canceled
// server-side, so they get a long forward timeout; everything else keeps the
// short default.
func TestForwardTimeout(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		want   time.Duration
	}{
		{
			name:   "promote POST gets the long timeout",
			method: http.MethodPost,
			path:   "/apps/myapp/releases/RABCDEFGHIJ/promote",
			want:   promoteForwardTimeout,
		},
		{
			name:   "promote POST without a leading slash gets the long timeout",
			method: http.MethodPost,
			path:   "apps/myapp/releases/RABCDEFGHIJ/promote",
			want:   promoteForwardTimeout,
		},
		{
			name:   "GET on the promote path keeps the default",
			method: http.MethodGet,
			path:   "/apps/myapp/releases/RABCDEFGHIJ/promote",
			want:   defaultForwardTimeout,
		},
		{
			name:   "release GET keeps the default",
			method: http.MethodGet,
			path:   "/apps/myapp/releases/RABCDEFGHIJ",
			want:   defaultForwardTimeout,
		},
		{
			name:   "other POSTs keep the default",
			method: http.MethodPost,
			path:   "/apps/myapp/builds",
			want:   defaultForwardTimeout,
		},
		{
			name:   "promote path with a suffix keeps the default",
			method: http.MethodPost,
			path:   "/apps/myapp/releases/RABCDEFGHIJ/promote/extra",
			want:   defaultForwardTimeout,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "http://gateway.example/", nil)
			require.Equal(t, tc.want, forwardTimeout(r, tc.path))
		})
	}
}
