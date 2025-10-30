package middleware

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/httputil"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/gin-gonic/gin"
)

var staticAssetExtensions = map[string]struct{}{
	"css":         {},
	"gif":         {},
	"htm":         {},
	"html":        {},
	"ico":         {},
	"jpeg":        {},
	"jpg":         {},
	"js":          {},
	"map":         {},
	"mp3":         {},
	"mp4":         {},
	"ogg":         {},
	"otf":         {},
	"png":         {},
	"svg":         {},
	"ttf":         {},
	"txt":         {},
	"wav":         {},
	"webmanifest": {},
	"webp":        {},
	"woff":        {},
	"woff2":       {},
	"zip":         {},
}

// DebugLogging logs request and response bodies for debugging purposes.
// This middleware should be added early in the chain to capture all requests.
func DebugLogging(_ *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/api/v1/health" {
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
			logHeaders(gtwlog.TopicHTTPRequestHeaders, req.Header)
		}

		if logReqBody && req.Body != nil && httputil.IsJSONContentType(req.Header.Get("Content-Type")) {
			bodyBytes, err := io.ReadAll(req.Body)
			if err == nil {
				_ = req.Body.Close()
				req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				if len(bodyBytes) > 0 && !httputil.IsBinaryContent(req.Header.Get("Content-Type")) {
					gtwlog.DebugTopicf(gtwlog.TopicHTTPRequestBody, "len=%d body=%s", len(bodyBytes), httputil.TruncateString(bodyBytes, httputil.BodyTruncationLimit))
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
			logHeaders(gtwlog.TopicHTTPResponseHeaders, c.Writer.Header())
		}

		respJSON := logRespBody && httputil.IsJSONContentType(c.Writer.Header().Get("Content-Type"))
		if respJSON {
			writer = &bodyWriter{ResponseWriter: c.Writer, body: &bytes.Buffer{}}
			c.Writer = writer
		}
		c.Next()
		if respJSON && writer != nil {
			responseBody := writer.body.Bytes()
			if len(responseBody) > 0 && !httputil.IsBinaryContent(c.Writer.Header().Get("Content-Type")) {
				gtwlog.DebugTopicf(gtwlog.TopicHTTPResponseBody, "status=%d len=%d body=%s", c.Writer.Status(), len(responseBody), httputil.TruncateString(responseBody, httputil.BodyTruncationLimit))
			}
		}

	}
}

func logHeaders(topic string, headers http.Header) {
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

func shouldFilterHTTPLog(path string) bool {
	if path == "" {
		return false
	}

	// Always log API requests even if they contain dots (emails, versions, etc.).
	if strings.HasPrefix(path, "/api/") {
		return false
	}

	if strings.Contains(path, "/node_modules/") || strings.Contains(path, "/app/@") || strings.HasPrefix(path, "/app/src/") {
		return true
	}

	segment := path
	if lastSlash := strings.LastIndex(segment, "/"); lastSlash >= 0 {
		segment = segment[lastSlash+1:]
	}
	if idx := strings.IndexAny(segment, "?#"); idx >= 0 {
		segment = segment[:idx]
	}
	if segment == "" {
		return false
	}

	if dot := strings.LastIndex(segment, "."); dot != -1 && dot < len(segment)-1 {
		ext := strings.ToLower(segment[dot+1:])
		if _, ok := staticAssetExtensions[ext]; ok {
			return true
		}
	}

	// Default to logging when unsure to avoid missing critical request traces.
	return false
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
