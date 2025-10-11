package middleware

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/gin-gonic/gin"
)

// DebugLogging logs request and response bodies for debugging purposes.
// This middleware should be added early in the chain to capture all requests.
func DebugLogging(cfg *config.Config) gin.HandlerFunc {
	if cfg == nil {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	logHeaders := cfg.LogRequestHeaders
	logRequestBodies := cfg.LogRequestBodies
	logResponseBodies := cfg.LogResponseBodies
	maxBytes := cfg.LogBodyMaxBytes

	// Skip if no logging is enabled
	if !logHeaders && !logRequestBodies && !logResponseBodies {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		path := c.Request.URL.Path
		method := c.Request.Method

		// Skip health check endpoints
		if path == "/.gateway/api/health" {
			c.Next()
			return
		}

		// Log request headers
		if logHeaders {
			fmt.Printf("DEBUG REQUEST HEADERS %s %s:\n", method, path)
			for key, values := range c.Request.Header {
				for _, value := range values {
					fmt.Printf("  %s: %s\n", key, value)
				}
			}
		}

		// Log request body
		var requestBodyBytes []byte
		if logRequestBodies && c.Request.Body != nil {
			var err error
			requestBodyBytes, err = io.ReadAll(c.Request.Body)
			if err == nil {
				c.Request.Body.Close() //nolint:errcheck
				// Restore body for downstream handlers
				c.Request.Body = io.NopCloser(bytes.NewReader(requestBodyBytes))

				if len(requestBodyBytes) > 0 {
					// Skip logging binary formats
					ct := strings.ToLower(c.GetHeader("Content-Type"))
					isBinary := strings.Contains(ct, "gzip") ||
						strings.Contains(ct, "application/octet-stream") ||
						strings.Contains(ct, "application/x-tar") ||
						strings.Contains(ct, "application/zip")

					if !isBinary {
						logBody := requestBodyBytes
						if maxBytes > 0 && len(logBody) > maxBytes {
							logBody = append([]byte{}, logBody[:maxBytes]...)
							logBody = append(logBody, []byte("…(truncated)")...)
						}
						fmt.Printf("DEBUG REQUEST BODY %s %s len=%d body=%s\n", method, path, len(requestBodyBytes), string(logBody))
					} else {
						fmt.Printf("DEBUG REQUEST BODY %s %s len=%d (binary, not logged)\n", method, path, len(requestBodyBytes))
					}
				}
			}
		}

		// Capture response body if needed
		if logResponseBodies {
			// Create a response writer that captures the body
			writer := &bodyWriter{
				ResponseWriter: c.Writer,
				body:           &bytes.Buffer{},
			}
			c.Writer = writer

			// Process request
			c.Next()

			// Log response body (JSON only)
			responseBody := writer.body.Bytes()
			if len(responseBody) > 0 {
				ct := strings.ToLower(c.Writer.Header().Get("Content-Type"))
				isJSON := strings.Contains(ct, "application/json")

				if isJSON {
					logBody := responseBody
					if maxBytes > 0 && len(logBody) > maxBytes {
						logBody = append([]byte{}, logBody[:maxBytes]...)
						logBody = append(logBody, []byte("…(truncated)")...)
					}
					fmt.Printf("DEBUG RESPONSE BODY %s %s status=%d len=%d body=%s\n", method, path, c.Writer.Status(), len(responseBody), string(logBody))
				}
			}
		} else {
			// No response logging, just continue
			c.Next()
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
