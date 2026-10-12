package auditverify

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// HashV2FromEnv pins where hash v2 starts (see Options.HashV2From).
const HashV2FromEnv = "AUDIT_HASH_V2_FROM"

// ParseChainIndex parses a chain index: a whole number, 0 or more.
func ParseChainIndex(value string) (int64, error) {
	index, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || index < 0 {
		return 0, fmt.Errorf("expected a chain index (a whole number, 0 or more), got %q", value)
	}
	return index, nil
}

// PinnedHashV2StartFromEnv returns the hash v2 start pinned by AUDIT_HASH_V2_FROM (nil when it isn't set).
func PinnedHashV2StartFromEnv() (*int64, error) {
	value := os.Getenv(HashV2FromEnv)
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	start, err := ParseChainIndex(value)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", HashV2FromEnv, err)
	}
	return &start, nil
}
