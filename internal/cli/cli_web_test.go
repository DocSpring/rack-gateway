package cli

import (
	"strings"
	"testing"
)

func TestLaunchBrowserValidation(t *testing.T) {
	t.Run("fails closed without allowed hosts", func(t *testing.T) {
		err := launchBrowser("https://gateway.example.com/app", nil)
		if err == nil {
			t.Fatal("expected browser launch validation to fail without allowed hosts")
		}
		if !strings.Contains(err.Error(), "no configured gateway hosts") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects host outside configured gateways", func(t *testing.T) {
		err := launchBrowser("https://evil.example.com/app", []string{"gateway.example.com"})
		if err == nil {
			t.Fatal("expected host validation to fail")
		}
		if !strings.Contains(err.Error(), "URL host must match a configured gateway") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects invalid scheme before launching", func(t *testing.T) {
		err := launchBrowser("file:///tmp/index.html", []string{"gateway.example.com"})
		if err == nil {
			t.Fatal("expected invalid scheme to be rejected")
		}
		if !strings.Contains(err.Error(), "invalid URL scheme") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
