package middleware

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/httputil"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// bodyWriter wraps gin.ResponseWriter to capture response body
type bodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *bodyWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *bodyWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

func shouldLogDebugTopics(path string) (logInfo, logHeaders, logBody, logRespHeaders, logRespBody bool) {
	if path == "/api/v1/health" {
		return false, false, false, false, false
	}

	logInfo = gtwlog.TopicEnabled(gtwlog.TopicHTTPRequestInfo)
	logHeaders = gtwlog.TopicEnabled(gtwlog.TopicHTTPRequestHeaders)
	logBody = gtwlog.TopicEnabled(gtwlog.TopicHTTPRequestBody)
	logRespHeaders = gtwlog.TopicEnabled(gtwlog.TopicHTTPResponseHeaders)
	logRespBody = gtwlog.TopicEnabled(gtwlog.TopicHTTPResponseBody)

	if !logInfo && !logHeaders && !logBody && !logRespHeaders && !logRespBody {
		return false, false, false, false, false
	}

	if shouldFilterHTTPLog(path) {
		return false, false, false, false, false
	}

	return logInfo, logHeaders, logBody, logRespHeaders, logRespBody
}

func logRequestInfo(req *http.Request, path string, logInfo, logReqHeaders bool) {
	if logInfo {
		gtwlog.DebugTopicf(gtwlog.TopicHTTPRequestInfo, "%s %s", req.Method, path)
	}
	if logReqHeaders {
		logHeadersToTopic(gtwlog.TopicHTTPRequestHeaders, req.Header)
	}
}

func logHeadersToTopic(topic string, headers http.Header) {
	if len(headers) == 0 {
		return
	}
	var builder strings.Builder
	for key, values := range headers {
		for _, value := range values {
			builder.WriteString(key)
			builder.WriteString(": ")
			builder.WriteString(value)
			builder.WriteByte('\n')
		}
	}
	out := strings.TrimSuffix(builder.String(), "\n")
	if out != "" {
		gtwlog.DebugTopicf(topic, "\n%s", out)
	}
}

func logRequestBody(req *http.Request, logBody bool) {
	if !logBody || req.Body == nil {
		return
	}
	if !httputil.IsJSONContentType(req.Header.Get("Content-Type")) {
		return
	}
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		gtwlog.Warnf("failed to read request body: %v", err)
		return
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	if len(bodyBytes) > 0 && !httputil.IsBinaryContent(req.Header.Get("Content-Type")) {
		gtwlog.DebugTopicf(
			gtwlog.TopicHTTPRequestBody,
			"len=%d body=%s",
			len(bodyBytes),
			httputil.TruncateString(bodyBytes, httputil.BodyTruncationLimit),
		)
	}
}

func setupResponseBodyLogging(c *gin.Context, logRespBody bool) *bodyWriter {
	if !logRespBody {
		return nil
	}
	writer := &bodyWriter{ResponseWriter: c.Writer, body: &bytes.Buffer{}}
	c.Writer = writer
	return writer
}

func logResponseHeaders(c *gin.Context, logRespHeaders bool) {
	if logRespHeaders {
		logHeadersToTopic(gtwlog.TopicHTTPResponseHeaders, c.Writer.Header())
	}
}

func logResponseBody(c *gin.Context, logRespBody bool) {
	if !logRespBody {
		return
	}
	if !httputil.IsJSONContentType(c.Writer.Header().Get("Content-Type")) {
		return
	}
	writer := &bodyWriter{ResponseWriter: c.Writer, body: &bytes.Buffer{}}
	c.Writer = writer
	c.Next()
	responseBody := writer.body.Bytes()
	if len(responseBody) > 0 && !httputil.IsBinaryContent(c.Writer.Header().Get("Content-Type")) {
		gtwlog.DebugTopicf(
			gtwlog.TopicHTTPResponseBody,
			"status=%d len=%d body=%s",
			c.Writer.Status(),
			len(responseBody),
			httputil.TruncateString(responseBody, httputil.BodyTruncationLimit),
		)
	}
}
