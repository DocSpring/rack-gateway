package proxy

import (
	"net/http"
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

// extendDeadlines lifts the server-wide read and write timeouts for the requests that legitimately take longer:
// a promote, whose response can take minutes (the rack applies the whole release first), and a build archive
// upload, which the gateway reads in full from the client and then sends to the rack. Without this the gateway
// would fail to read the upload, or would wait for the rack's response and then fail to write it, and the client
// would see the connection close (a 502 behind a proxy) while the promote had succeeded. The server starts the
// write deadline when it reads the request's headers, so for an upload it covers reading the archive too. Other
// requests keep the defaults.
func extendDeadlines(w http.ResponseWriter, r *http.Request, rackPath string) {
	controller := http.NewResponseController(w)
	path := "/" + strings.TrimLeft(rackPath, "/")
	var readTimeout time.Duration
	if isObjectUpload(r, path) {
		readTimeout = uploadReadTimeout
		if err := controller.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			gtwlog.Warnf("proxy: can't extend the read deadline for %s %s: %v", r.Method, path, err)
		}
	}
	timeout := forwardTimeout(r, path)
	if readTimeout == 0 && timeout <= defaultForwardTimeout {
		return
	}
	if err := controller.SetWriteDeadline(time.Now().Add(readTimeout + timeout + deadlineMargin)); err != nil {
		gtwlog.Warnf("proxy: can't extend the write deadline for %s %s: %v", r.Method, path, err)
	}
}
