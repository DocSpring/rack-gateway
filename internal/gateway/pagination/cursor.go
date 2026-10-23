package pagination

import (
	"encoding/base64"
	"fmt"
	"strconv"
)

// Cursor is a pagination cursor: the ID of an item at the edge of a page, for listings ordered by ID.
type Cursor struct {
	ID int64
}

// ParseCursor parses a cursor made by Encode, or a bare ID ("12345"). An encoded cursor is never all digits: base64
// of a digit starts with M, N or O.
func ParseCursor(s string) (*Cursor, error) {
	if s == "" {
		return nil, nil
	}
	if id, err := strconv.ParseInt(s, 10, 64); err == nil {
		return &Cursor{ID: id}, nil
	}
	decoded, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor: %w", err)
	}
	id, err := strconv.ParseInt(string(decoded), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor: %w", err)
	}
	return &Cursor{ID: id}, nil
}

// Encode returns a base64-encoded cursor string
func (c *Cursor) Encode() string {
	if c == nil {
		return ""
	}
	return base64.URLEncoding.EncodeToString([]byte(strconv.FormatInt(c.ID, 10)))
}
