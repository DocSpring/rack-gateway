package sentryutil

import (
	"net/http"

	"github.com/getsentry/sentry-go"
)

// WithHTTPRequestScope configures a Sentry scope with HTTP request context, optional user,
// and arbitrary tags before invoking capture. capture is invoked within the configured scope.
func WithHTTPRequestScope(r *http.Request, userEmail string, tags map[string]string, capture func()) {
	if capture == nil {
		return
	}

	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(sentry.LevelError)
		if r != nil {
			scope.SetRequest(r)
			scope.SetTag("http_method", r.Method)
			scope.SetTag("http_path", r.URL.Path)
		}
		if userEmail != "" {
			scope.SetUser(sentry.User{Email: userEmail})
		}
		for key, value := range tags {
			if key == "" || value == "" {
				continue
			}
			scope.SetTag(key, value)
		}

		capture()
	})
}
