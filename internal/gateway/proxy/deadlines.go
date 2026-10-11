package proxy

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

const (
	// deadlineMargin is how much longer than its upstream timeout a long request may keep the client connection,
	// so the gateway can still write the rack's response (or its own error) after waiting the full timeout.
	deadlineMargin = 30 * time.Second

	// uploadReadTimeout bounds reading an uploaded build archive from the client, which can take longer than the
	// server-wide read timeout over a slow link.
	uploadReadTimeout = 10 * time.Minute
)

var uploadPathPattern = regexp.MustCompile(`^/apps/[^/]+/objects/`)

// extendDeadlines lifts the server-wide read and write timeouts for the requests that legitimately take longer:
// a promote, whose response can take minutes (the rack applies the whole release first), and an archive upload.
// Without this the gateway would wait for the rack's response and then fail to write it, and the client would see
// the connection close (a 502 behind a proxy) while the promote had succeeded. Other requests keep the defaults.
func extendDeadlines(w http.ResponseWriter, r *http.Request, rackPath string) {
	controller := http.NewResponseController(w)
	path := "/" + strings.TrimLeft(rackPath, "/")
	if timeout := forwardTimeout(r, path); timeout > defaultForwardTimeout {
		if err := controller.SetWriteDeadline(time.Now().Add(timeout + deadlineMargin)); err != nil {
			gtwlog.Warnf("proxy: can't extend the write deadline for %s %s: %v", r.Method, path, err)
		}
	}
	if r.Method == http.MethodPost && uploadPathPattern.MatchString(path) {
		if err := controller.SetReadDeadline(time.Now().Add(uploadReadTimeout)); err != nil {
			gtwlog.Warnf("proxy: can't extend the read deadline for %s %s: %v", r.Method, path, err)
		}
	}
}
