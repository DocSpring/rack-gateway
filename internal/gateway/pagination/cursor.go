package pagination

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Cursor represents a pagination cursor that can be ID-only or timestamp+ID composite
type Cursor struct {
	Timestamp *time.Time // Optional: for time-based sorts
	ID        int64      // Required: for stable ordering
}

// ParseCursor parses a cursor string in format "2025-11-01T12:00:00_12345" or "12345"
func ParseCursor(s string) (*Cursor, error) {
	if s == "" {
		return nil, nil
	}

	// Decode base64 if it looks encoded
	decoded := s
	if isBase64(s) {
		b, err := base64.URLEncoding.DecodeString(s)
		if err == nil {
			decoded = string(b)
		}
	}

	// Try composite format: "timestamp_id"
	parts := strings.Split(decoded, "_")
	if len(parts) == 2 {
		timestamp, err := time.Parse(time.RFC3339, parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid cursor timestamp: %w", err)
		}
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor ID: %w", err)
		}
		return &Cursor{Timestamp: &timestamp, ID: id}, nil
	}

	// Try ID-only format
	if len(parts) == 1 {
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor: %w", err)
		}
		return &Cursor{ID: id}, nil
	}

	return nil, fmt.Errorf("invalid cursor format")
}

// Encode returns a base64-encoded cursor string
func (c *Cursor) Encode() string {
	if c == nil {
		return ""
	}

	var raw string
	if c.Timestamp != nil {
		raw = fmt.Sprintf("%s_%d", c.Timestamp.Format(time.RFC3339), c.ID)
	} else {
		raw = strconv.FormatInt(c.ID, 10)
	}

	return base64.URLEncoding.EncodeToString([]byte(raw))
}

// isBase64 checks if a string looks like base64
func isBase64(s string) bool {
	if len(s) == 0 {
		return false
	}
	// Simple heuristic: if it contains non-numeric/non-underscore chars, likely base64
	for _, r := range s {
		if (r < '0' || r > '9') && r != '_' && r != '-' {
			return true
		}
	}
	return false
}
