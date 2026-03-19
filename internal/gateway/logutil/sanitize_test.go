package logutil

import "testing"

func TestSanitizeForLog(t *testing.T) {
	input := "hello\x00world\nok\tstill-here"
	want := "helloworldok\tstill-here"

	if got := SanitizeForLog(input); got != want {
		t.Fatalf("SanitizeForLog(%q) = %q, want %q", input, got, want)
	}
}
