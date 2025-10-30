package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
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
		path := c.Request.URL.RequestURI()
		logReqInfo, logReqHeaders, logReqBody, logRespHeaders, logRespBody := shouldLogDebugTopics(path)

		if !logReqInfo && !logReqHeaders && !logReqBody && !logRespHeaders && !logRespBody {
			c.Next()
			return
		}

		logRequestInfo(c.Request, path, logReqInfo, logReqHeaders)
		logRequestBody(c.Request, logReqBody)

		writer := setupResponseBodyLogging(c, logRespBody)

		c.Next()

		logResponseHeaders(c, logRespHeaders)

		if logRespBody && writer != nil {
			logResponseBody(c, logRespBody)
		}
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

	if strings.Contains(path, "/node_modules/") || strings.Contains(path, "/app/@") ||
		strings.HasPrefix(path, "/app/src/") {
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
