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

// unreadBody is a request body that records whether anything read it.
type unreadBody struct{ read bool }

func (b *unreadBody) Read([]byte) (int, error) {
	b.read = true
	return 0, io.EOF
}

// A build import uploads an exported build, not an archive uploaded under an approval, so an approval never covers
// it. It's refused without the gateway reading the import.
func TestTokenBuildImportIsRefusedUnread(t *testing.T) {
	f := newBindingFixture(t)
	body := &unreadBody{}
	r := httptest.NewRequest(http.MethodPost, "/apps/myapp/builds/import", body)
	allowed, _, err := f.h.evaluateAPITokenPermission(r, f.authUser, rbac.ResourceBuild, rbac.ActionCreate)
	require.False(t, allowed)
	var approvalErr *deployApprovalError
	require.ErrorAs(t, err, &approvalErr)
	require.Equal(t, http.StatusForbidden, approvalErr.status)
	require.False(t, body.read, "the gateway must not read an imported build")
}
