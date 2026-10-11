package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/httputil"
)

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

// countingReader counts the bytes read through it.
type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// Logging a request body reads only the part that's logged, and the handler still gets the whole body.
func TestLogRequestBodyReadsOnlyWhatItLogs(t *testing.T) {
	body := `{"data":"` + strings.Repeat("a", 4*httputil.BodyTruncationLimit) + `"}`
	source := &countingReader{r: strings.NewReader(body)}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/example", io.NopCloser(source))
	req.Header.Set("Content-Type", "application/json")

	logRequestBody(req, true)
	require.LessOrEqual(t, source.n, httputil.BodyTruncationLimit+1)

	got, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Equal(t, body, string(got))
}
