package middleware

import (
	"bytes"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/debugredact"
	"github.com/DocSpring/rack-gateway/internal/gateway/httputil"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

func shouldLogDebugTopics(path string) (logInfo, logHeaders, logBody, logRespHeaders bool) {
	if path == "/api/v1/health" {
		return false, false, false, false
	}

	logInfo = gtwlog.TopicEnabled(gtwlog.TopicHTTPRequestInfo)
	logHeaders = gtwlog.TopicEnabled(gtwlog.TopicHTTPRequestHeaders)
	logBody = gtwlog.TopicEnabled(gtwlog.TopicHTTPRequestBody)
	logRespHeaders = gtwlog.TopicEnabled(gtwlog.TopicHTTPResponseHeaders)

	if !logInfo && !logHeaders && !logBody && !logRespHeaders {
		return false, false, false, false
	}

	if shouldFilterHTTPLog(path) {
		return false, false, false, false
	}

	return logInfo, logHeaders, logBody, logRespHeaders
}

func logRequestInfo(req *http.Request, path string, logInfo, logReqHeaders bool) {
	if logInfo {
		gtwlog.DebugTopicf(gtwlog.TopicHTTPRequestInfo, "%s %s", req.Method, path)
	}
	if logReqHeaders {
		logHeadersToTopic(gtwlog.TopicHTTPRequestHeaders, req.Header)
	}
}

// logHeadersToTopic logs headers with credentials (session and API tokens, cookies, CSRF tokens, MFA codes, a
// redirect's login code) redacted, so turning on debug logging doesn't put them in the logs.
func logHeadersToTopic(topic string, headers http.Header) {
	if out := debugredact.Headers(headers); out != "" {
		gtwlog.DebugTopicf(topic, "\n%s", out)
	}
}

// logRequestBody logs a JSON request body with sensitive values (passwords, tokens, MFA codes, env values) redacted.
// It reads only as much as it logs and puts that back in front of the rest, so a large body still streams to the
// handler instead of being held in memory here.
func logRequestBody(req *http.Request, logBody bool) {
	if !logBody || req.Body == nil {
		return
	}
	if !httputil.IsJSONContentType(req.Header.Get("Content-Type")) {
		return
	}
	head, err := io.ReadAll(io.LimitReader(req.Body, httputil.BodyTruncationLimit+1))
	req.Body = replayedBody{Reader: io.MultiReader(bytes.NewReader(head), req.Body), Closer: req.Body}
	if err != nil {
		gtwlog.Warnf("failed to read request body: %v", err)
		return
	}
	if len(head) > 0 && !httputil.IsBinaryContent(req.Header.Get("Content-Type")) {
		gtwlog.DebugTopicf(
			gtwlog.TopicHTTPRequestBody,
			"len=%d body=%s",
			req.ContentLength,
			debugredact.Body(head, httputil.BodyTruncationLimit),
		)
	}
}

// replayedBody is a request body whose first bytes were already read, and are read again before the rest.
type replayedBody struct {
	io.Reader
	io.Closer
}

func logResponseHeaders(c *gin.Context, logRespHeaders bool) {
	if logRespHeaders {
		logHeadersToTopic(gtwlog.TopicHTTPResponseHeaders, c.Writer.Header())
	}
}
