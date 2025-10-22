package httputil

import (
	"net/http"
	"strings"
)

// BodyTruncationLimit is the maximum number of bytes to include in log output
const BodyTruncationLimit = 16384

// TruncateBytes truncates a byte slice to the specified limit, appending a truncation indicator if needed.
func TruncateBytes(body []byte, limit int) []byte {
	if limit <= 0 || len(body) <= limit {
		return body
	}
	truncated := append([]byte{}, body[:limit]...)
	return append(truncated, []byte("…(truncated)")...)
}

// TruncateString truncates a byte slice to the specified limit and returns it as a string with truncation indicator if needed.
func TruncateString(body []byte, limit int) string {
	if limit <= 0 || len(body) <= limit {
		return string(body)
	}
	return string(body[:limit]) + "…(truncated)"
}

// IsBinaryContent returns true if the content type indicates binary data that should not be logged as text.
func IsBinaryContent(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if ct == "" {
		return false
	}
	return strings.Contains(ct, "application/octet-stream") ||
		strings.Contains(ct, "application/x-tar") ||
		strings.Contains(ct, "application/zip") ||
		strings.Contains(ct, "gzip")
}

// IsJSONContentType returns true if the content type is JSON or a JSON variant.
func IsJSONContentType(contentType string) bool {
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

// CopyHeaders copies HTTP headers from src to dst, excluding headers in the skip list.
// Header names are compared case-insensitively.
func CopyHeaders(dst http.Header, src http.Header, skip ...string) {
	skipMap := make(map[string]struct{}, len(skip))
	for _, h := range skip {
		skipMap[strings.ToLower(h)] = struct{}{}
	}

	for key, values := range src {
		if _, shouldSkip := skipMap[strings.ToLower(key)]; shouldSkip {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}
