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

		logHTTP := gtwlog.TopicEnabled(gtwlog.TopicHTTP)
		logHTTPRequest := gtwlog.TopicEnabled(gtwlog.TopicHTTPRequest)
		logReqHeaders := gtwlog.TopicEnabled(gtwlog.TopicHTTPRequestHeaders)
		logReqBody := gtwlog.TopicEnabled(gtwlog.TopicHTTPRequestBody)
		logHTTPResp := gtwlog.TopicEnabled(gtwlog.TopicHTTPResponse)
		logRespHeaders := gtwlog.TopicEnabled(gtwlog.TopicHTTPResponseHeaders)
		logRespBody := gtwlog.TopicEnabled(gtwlog.TopicHTTPResponseBody)

		if !logHTTP && !logHTTPRequest && !logReqHeaders && !logReqBody && !logHTTPResp && !logRespHeaders && !logRespBody {
			c.Next()
			return
		}

		start := time.Now()
		req := c.Request

		if logHTTP {
			gtwlog.DebugTopicf(gtwlog.TopicHTTP, "%s %s", req.Method, req.URL.RequestURI())
		}
		if logHTTPRequest {
			gtwlog.DebugTopicf(gtwlog.TopicHTTPRequest, "%s %s", req.Method, req.URL.RequestURI())
		}
		if logReqHeaders {
			for key, values := range req.Header {
				for _, value := range values {
					gtwlog.DebugTopicf(gtwlog.TopicHTTPRequestHeaders, "%s: %s", key, value)
				}
			}
		}

		if (logReqBody || logHTTPRequest) && req.Body != nil {
			bodyBytes, err := io.ReadAll(req.Body)
			if err == nil {
				_ = req.Body.Close()
				req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				if len(bodyBytes) > 0 && !isBinaryContent(req.Header.Get("Content-Type")) {
					if logReqBody {
						gtwlog.DebugTopicf(gtwlog.TopicHTTPRequestBody, "len=%d body=%s", len(bodyBytes), truncateBody(bodyBytes))
					} else if logHTTPRequest {
						gtwlog.DebugTopicf(gtwlog.TopicHTTPRequest, "len=%d body=%s", len(bodyBytes), truncateBody(bodyBytes))
					}
				}
			} else {
				gtwlog.Warnf("failed to read request body: %v", err)
			}
		}

		var writer *bodyWriter
		if logRespBody || logHTTPResp {
			writer = &bodyWriter{ResponseWriter: c.Writer, body: &bytes.Buffer{}}
			c.Writer = writer
		}

		c.Next()

		if logRespHeaders {
			for key, values := range c.Writer.Header() {
				for _, value := range values {
					gtwlog.DebugTopicf(gtwlog.TopicHTTPResponseHeaders, "%s: %s", key, value)
				}
			}
		}

		if (logRespBody || logHTTPResp) && writer != nil {
			responseBody := writer.body.Bytes()
			if len(responseBody) > 0 && !isBinaryContent(c.Writer.Header().Get("Content-Type")) {
				if logRespBody {
					gtwlog.DebugTopicf(gtwlog.TopicHTTPResponseBody, "status=%d len=%d body=%s", c.Writer.Status(), len(responseBody), truncateBody(responseBody))
				} else if logHTTPResp {
					gtwlog.DebugTopicf(gtwlog.TopicHTTPResponse, "status=%d len=%d body=%s", c.Writer.Status(), len(responseBody), truncateBody(responseBody))
				}
			}
		}

		if logHTTP {
			gtwlog.DebugTopicf(gtwlog.TopicHTTP, "response %d %s in %s", c.Writer.Status(), req.URL.RequestURI(), time.Since(start))
		}
		if logHTTPResp {
			gtwlog.DebugTopicf(gtwlog.TopicHTTPResponse, "response %d %s in %s", c.Writer.Status(), req.URL.RequestURI(), time.Since(start))
		}
	}
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
