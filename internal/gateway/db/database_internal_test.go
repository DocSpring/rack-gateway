package db

import (
	"net/netip"
	"testing"
)

func TestNullableIPReturnsCanonicalString(t *testing.T) {
	t.Helper()
	val := nullableIP("203.0.113.10")

	addr, err := netip.ParseAddr(val.(string))
	if err != nil {
		t.Fatalf("expected canonical IP string, got parse error: %v", err)
	}
	if addr.String() != "203.0.113.10" {
		t.Fatalf("expected canonical addr 203.0.113.10, got %s", addr.String())
	}
}

func TestNullableIPHandlesEmpty(t *testing.T) {
	if val := nullableIP("   "); val != nil {
		t.Fatalf("expected nil for blank IP, got %v", val)
	}
}
