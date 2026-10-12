package proxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// uploadHead is how much of an archive the client sends before it waits for the rack to start reading.
const uploadHead = 64 << 10

type receivedUpload struct {
	body          []byte
	contentLength int64
	chunked       bool
}

// An archive upload streams through the gateway: the rack starts receiving it before the client has sent all of it,
// gets every byte, and sees the client's Content-Length. The archive is larger than any body the gateway buffers.
func TestArchiveUploadStreamsToTheRack(t *testing.T) {
	archive := make([]byte, maxBufferedBodyBytes+uploadHead)
	for i := range archive {
		archive[i] = byte(i % 251)
	}

	rackReading := make(chan struct{})
	uploads := make(chan receivedUpload, 1)
	gateway := serveProxyAsAdmin(t, func(w http.ResponseWriter, r *http.Request) {
		head := make([]byte, 1)
		n, _ := io.ReadFull(r.Body, head)
		close(rackReading)
		rest, _ := io.ReadAll(r.Body)
		uploads <- receivedUpload{
			body:          append(head[:n], rest...),
			contentLength: r.ContentLength,
			chunked:       len(r.TransferEncoding) > 0,
		}
		_, _ = io.WriteString(w, `{"url":"object://app/tmp/source.tgz"}`)
	})

	body, writer := io.Pipe()
	go func() {
		// The rest is only sent once the rack is reading. A gateway that read the whole archive before forwarding
		// it would wait for the rest forever, so the upload fails instead.
		_, _ = writer.Write(archive[:uploadHead])
		select {
		case <-rackReading:
			_, _ = writer.Write(archive[uploadHead:])
			_ = writer.Close()
		case <-time.After(10 * time.Second):
			_ = writer.CloseWithError(errors.New("the rack got nothing before the client finished sending"))
		}
	}()
	req, err := http.NewRequest(http.MethodPost, gateway+"/apps/app/objects/tmp/source.tgz", body)
	require.NoError(t, err)
	req.ContentLength = int64(len(archive))
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := http.DefaultClient.Do(req)
	requireOK(t, resp, err)

	upload := <-uploads
	require.Equal(t, int64(len(archive)), upload.contentLength)
	require.False(t, upload.chunked, "the rack should get the client's Content-Length, not a chunked body")
	require.True(t, bytes.Equal(archive, upload.body), "the rack must receive the archive intact")
}

// Every other body is read into memory, so it's capped: one over the limit gets a 413 and never reaches the rack,
// whether or not the client said how long it is. One at the limit is forwarded.
func TestOversizedRequestBodyIsRefused(t *testing.T) {
	forwarded := make(chan int64, 3)
	h, database, mgr, cleanup := newProxyWithRackServer(t, func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		forwarded <- n
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"B1"}`)
	})
	t.Cleanup(cleanup)
	require.NoError(t, mgr.SaveUser("admin@test.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}))

	cases := []struct {
		name          string
		size          int
		unknownLength bool
		want          int
	}{
		{name: "at the limit", size: maxBufferedBodyBytes, want: http.StatusOK},
		{name: "over the limit", size: maxBufferedBodyBytes + 1, want: http.StatusRequestEntityTooLarge},
		{
			name: "over the limit, sent chunked", size: maxBufferedBodyBytes + 1, unknownLength: true,
			want: http.StatusRequestEntityTooLarge,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			form := "url=object://myapp/tmp/source.tgz&description="
			req := withPath(requestAs(t, database, "admin@test.com"), http.MethodPost, "/apps/myapp/builds")
			req = withBody(req, form+strings.Repeat("a", tc.size-len(form)), "application/x-www-form-urlencoded")
			if tc.unknownLength {
				req.ContentLength = -1
			}

			rr := httptest.NewRecorder()
			h.ProxyToRack(rr, req)
			require.Equal(t, tc.want, rr.Code, rr.Body.String())
			if tc.want == http.StatusRequestEntityTooLarge {
				require.Contains(t, rr.Body.String(), "request body too large")
			}
		})
	}
	require.Len(t, forwarded, 1, "only the body within the limit reaches the rack")
	require.Equal(t, int64(maxBufferedBodyBytes), <-forwarded)
}

// A release create is read to check its env before it's forwarded, so an oversized one is refused there.
func TestOversizedReleaseCreateIsRefused(t *testing.T) {
	f := newProtectedEnvFixture(t)
	req, _ := f.request(t, f.deployer, strings.Repeat("A", maxBufferedBodyBytes), "", "", stepUpAgeFresh)
	rr := f.proxy(req)
	require.Equal(t, http.StatusRequestEntityTooLarge, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "request body too large")
	require.Empty(t, f.rack.releases())
}

// An API token's build is read to find its deploy approval while its permission is checked, so an oversized one is
// refused there.
func TestOversizedTokenBuildIsRefused(t *testing.T) {
	f := newBindingFixture(t)
	body := "url=" + approvedObjectURL + "&description=" + strings.Repeat("a", maxBufferedBodyBytes)

	r := httptest.NewRequest(http.MethodPost, buildPath, strings.NewReader(body))
	_, _, err := f.h.evaluateAPITokenPermission(r, f.authUser, rbac.ResourceBuild, rbac.ActionCreate)
	var approvalErr *deployApprovalError
	require.ErrorAs(t, err, &approvalErr)
	require.Equal(t, http.StatusRequestEntityTooLarge, approvalErr.status)

	req := httptest.NewRequest(http.MethodPost, buildPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	f.h.ProxyToRack(rr, req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, f.authUser)))
	require.Equal(t, http.StatusRequestEntityTooLarge, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "request body too large")
}

// The Convox SDK never sends a body on GET, HEAD or DELETE, so the gateway refuses one instead of holding it in
// memory and forwarding it, whether or not the client said how long it is.
func TestBodyOnABodylessMethodIsRefused(t *testing.T) {
	h, database, rack := newRecordingProxy(t)
	for _, unknownLength := range []bool{false, true} {
		req := withPath(requestAs(t, database, "deployer@test.com"), http.MethodGet, "/apps/myapp/processes")
		req = withBody(req, "service=web", "application/x-www-form-urlencoded")
		if unknownLength {
			req.ContentLength = -1
		}

		rr := httptest.NewRecorder()
		h.ProxyToRack(rr, req)
		require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
		require.Contains(t, rr.Body.String(), "request body not allowed")
	}
	require.Zero(t, rack.count(), "refused requests must not reach the rack")

	req := withPath(requestAs(t, database, "deployer@test.com"), http.MethodGet, "/apps/myapp/processes")
	rr := httptest.NewRecorder()
	h.ProxyToRack(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, "a GET without a body is forwarded: %s", rr.Body.String())
}

func TestReadBufferedBodyRefusesABodyOnHeadAndDelete(t *testing.T) {
	for _, method := range []string{http.MethodHead, http.MethodDelete} {
		_, err := readBufferedBody(httptest.NewRequest(method, "/apps/myapp", strings.NewReader("x")))
		require.ErrorIs(t, err, errBodyNotAllowed, method)
		require.Equal(t, http.StatusBadRequest, bodyErrorStatus(err, http.StatusInternalServerError))

		body, err := readBufferedBody(httptest.NewRequest(method, "/apps/myapp", nil))
		require.NoError(t, err, method)
		require.Empty(t, body)
	}
}

// A rack response the gateway filters is read into memory, so it's capped too: one over the limit gets a 502
// instead of being held. One at the limit is passed on.
func TestOversizedRackResponseIsRefused(t *testing.T) {
	size := maxBufferedResponseBytes
	h, database, mgr, cleanup := newProxyWithRackServer(t, func(w http.ResponseWriter, _ *http.Request) {
		// Whitespace keeps the release list valid JSON at any size.
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "["+strings.Repeat(" ", size-2)+"]")
	})
	t.Cleanup(cleanup)
	require.NoError(t, mgr.SaveUser("admin@test.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}))

	rr := httptest.NewRecorder()
	h.ProxyToRack(rr, withPath(requestAs(t, database, "admin@test.com"), http.MethodGet, "/apps/myapp/releases"))
	require.Equal(t, http.StatusOK, rr.Code, "a response at the limit is passed on")

	size = maxBufferedResponseBytes + 1
	rr = httptest.NewRecorder()
	h.ProxyToRack(rr, withPath(requestAs(t, database, "admin@test.com"), http.MethodGet, "/apps/myapp/releases"))
	require.Equal(t, http.StatusBadGateway, rr.Code)
	require.Contains(t, rr.Body.String(), "the rack's response is too large")
}

// When the rack's response breaks off after its status was passed on, the client has the status and part of the
// body, so the gateway just stops: an error body would be appended to the download.
func TestInterruptedResponseIsNotAppendedTo(t *testing.T) {
	const partial = "partial archive"
	h, database, mgr, cleanup := newProxyWithRackServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "1000")
		_, _ = io.WriteString(w, partial)
		rc := http.NewResponseController(w)
		_ = rc.Flush()
		if conn, _, err := rc.Hijack(); err == nil {
			_ = conn.Close()
		}
	})
	t.Cleanup(cleanup)
	require.NoError(t, mgr.SaveUser("admin@test.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}))

	rr := httptest.NewRecorder()
	h.ProxyToRack(rr, withPath(requestAs(t, database, "admin@test.com"), http.MethodGet, "/apps/myapp/builds/B1.tgz"))
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, partial, rr.Body.String())
}
