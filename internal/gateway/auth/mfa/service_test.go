package mfa

import (
	"testing"
	"time"
)

func TestNewServiceRequiresPepper(t *testing.T) {
	t.Parallel()

	if _, err := NewService(nil, "issuer", time.Hour, time.Minute, nil); err == nil {
		t.Fatalf("expected error when backup code pepper missing")
	}
}
