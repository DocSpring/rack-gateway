package middleware

import (
	"bytes"
	"io"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/gin-gonic/gin"
)

// DebugLogging logs request and response bodies for debugging purposes.
// This middleware should be added early in the chain to capture all requests.
func DebugLogging(_ *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/.gateway/api/health" {
			c.Next()
			return
		}

		logReqInfo := gtwlog.TopicEnabled(gtwlog.TopicHTTPRequestInfo)
		logReqHeaders := gtwlog.TopicEnabled(gtwlog.TopicHTTPRequestHeaders)
		logReqBody := gtwlog.TopicEnabled(gtwlog.TopicHTTPRequestBody)
		logRespHeaders := gtwlog.TopicEnabled(gtwlog.TopicHTTPResponseHeaders)
		logRespBody := gtwlog.TopicEnabled(gtwlog.TopicHTTPResponseBody)

		if !logReqInfo && !logReqHeaders && !logReqBody && !logRespHeaders && !logRespBody {
			c.Next()
			return
		}

		start := time.Now()
		req := c.Request

		path := req.URL.RequestURI()
		if shouldFilterHTTPLog(path) {
			logReqInfo = false
			logReqHeaders = false
			logReqBody = false
			logRespHeaders = false
			logRespBody = false
		}

		if logReqInfo {
			gtwlog.DebugTopicf(gtwlog.TopicHTTPRequestInfo, "%s %s", req.Method, path)
		}

		if logReqHeaders {
			var builder strings.Builder
			for key, values := range req.Header {
				for _, value := range values {
					builder.WriteString(key)
					builder.WriteString(": ")
					builder.WriteString(value)
					builder.WriteByte('\n')
				}
			}
			headers := strings.TrimSuffix(builder.String(), "\n")
			if headers != "" {
				gtwlog.DebugTopicf(gtwlog.TopicHTTPRequestHeaders, "\n%s", headers)
			}
		}

		if logReqBody && req.Body != nil && isJSONContentType(req.Header.Get("Content-Type")) {
			bodyBytes, err := io.ReadAll(req.Body)
			if err == nil {
				_ = req.Body.Close()
				req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				if len(bodyBytes) > 0 && !isBinaryContent(req.Header.Get("Content-Type")) {
					gtwlog.DebugTopicf(gtwlog.TopicHTTPRequestBody, "len=%d body=%s", len(bodyBytes), truncateBody(bodyBytes))
				}
			} else {
				gtwlog.Warnf("failed to read request body: %v", err)
			}
		}

		var writer *bodyWriter
		if logRespBody {
			writer = &bodyWriter{ResponseWriter: c.Writer, body: &bytes.Buffer{}}
			c.Writer = writer
		}

		c.Next()

		if logRespHeaders {
			var builder strings.Builder
			for key, values := range c.Writer.Header() {
				for _, value := range values {
					builder.WriteString(key)
					builder.WriteString(": ")
					builder.WriteString(value)
					builder.WriteByte('\n')
				}
			}
			headers := strings.TrimSuffix(builder.String(), "\n")
			if headers != "" {
				gtwlog.DebugTopicf(gtwlog.TopicHTTPResponseHeaders, "\n%s", headers)
			}
		}

		respJSON := logRespBody && isJSONContentType(c.Writer.Header().Get("Content-Type"))
		if respJSON {
			writer = &bodyWriter{ResponseWriter: c.Writer, body: &bytes.Buffer{}}
			c.Writer = writer
		}
		c.Next()
		if respJSON && writer != nil {
			responseBody := writer.body.Bytes()
			if len(responseBody) > 0 && !isBinaryContent(c.Writer.Header().Get("Content-Type")) {
				gtwlog.DebugTopicf(gtwlog.TopicHTTPResponseBody, "status=%d len=%d body=%s", c.Writer.Status(), len(responseBody), truncateBody(responseBody))
			}
		}

		_ = start
	}
}

func shouldFilterHTTPLog(path string) bool {
	if strings.Contains(path, "/node_modules/") || strings.Contains(path, "/web/@") {
		return true
	}

	idx := strings.LastIndex(path, ".")
	if idx == -1 {
		return false
	}

	lastSlash := strings.LastIndex(path, "/")
	if lastSlash >= 0 && idx < lastSlash {
		return false
	}

	ext := path[idx+1:]
	if ext == "" {
		return false
	}

	return !strings.Contains(ext, "/")
}

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

func truncateBody(body []byte) string {
	const maxBytes = 16384
	if len(body) <= maxBytes {
		return string(body)
	}
	return string(body[:maxBytes]) + "…(truncated)"
}

func isBinaryContent(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if ct == "" {
		return false
	}
	return strings.Contains(ct, "application/octet-stream") ||
		strings.Contains(ct, "application/x-tar") ||
		strings.Contains(ct, "application/zip") ||
		strings.Contains(ct, "gzip")
}

func isJSONContentType(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if ct == "" {
		return false
	}
	if idx := strings.IndexByte(ct, ';'); idx >= 0 {
		ct = strings.TrimSpace(ct[:idx])
	}
	if ct == "application/json" {
		return true
	}
	return strings.HasSuffix(ct, "+json")
}
