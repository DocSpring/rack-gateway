package httputil

import (
	"net/http"
	"strings"
	"testing"
)

type truncateTestCase struct {
	name     string
	input    []byte
	limit    int
	expected string
}

func TestTruncateBytes(t *testing.T) {
	tests := []truncateTestCase{
		{
			name:     "empty input",
			input:    []byte{},
			limit:    10,
			expected: "",
		},
		{
			name:     "input shorter than limit",
			input:    []byte("hello"),
			limit:    10,
			expected: "hello",
		},
		{
			name:     "input exactly at limit",
			input:    []byte("helloworld"),
			limit:    10,
			expected: "helloworld",
		},
		{
			name:     "input longer than limit",
			input:    []byte("hello world, this is a test"),
			limit:    10,
			expected: "hello worl…(truncated)",
		},
		{
			name:     "zero limit returns original",
			input:    []byte("hello"),
			limit:    0,
			expected: "hello",
		},
		{
			name:     "negative limit returns original",
			input:    []byte("hello"),
			limit:    -1,
			expected: "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TruncateBytes(tt.input, tt.limit)
			if string(result) != tt.expected {
				t.Errorf("TruncateBytes() = %q, want %q", string(result), tt.expected)
			}
		})
	}
}

func TestTruncateString(t *testing.T) {
	tests := []truncateTestCase{
		{
			name:     "empty input",
			input:    []byte{},
			limit:    10,
			expected: "",
		},
		{
			name:     "input shorter than limit",
			input:    []byte("hello"),
			limit:    10,
			expected: "hello",
		},
		{
			name:     "input exactly at limit",
			input:    []byte("helloworld"),
			limit:    10,
			expected: "helloworld",
		},
		{
			name:     "input longer than limit",
			input:    []byte("hello world, this is a test"),
			limit:    10,
			expected: "hello worl…(truncated)",
		},
		{
			name:     "zero limit returns original",
			input:    []byte("hello"),
			limit:    0,
			expected: "hello",
		},
		{
			name:     "negative limit returns original",
			input:    []byte("hello"),
			limit:    -1,
			expected: "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TruncateString(tt.input, tt.limit)
			if result != tt.expected {
				t.Errorf("TruncateString() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestIsBinaryContent(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        bool
	}{
		{"empty", "", false},
		{"json", "application/json", false},
		{"text", "text/plain", false},
		{"octet-stream", "application/octet-stream", true},
		{"tar", "application/x-tar", true},
		{"zip", "application/zip", true},
		{"gzip", "application/gzip", true},
		{"gzip with charset", "application/gzip; charset=utf-8", true},
		{"case insensitive", "APPLICATION/ZIP", true},
		{"with spaces", "  application/zip  ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsBinaryContent(tt.contentType); got != tt.want {
				t.Errorf("IsBinaryContent(%q) = %v, want %v", tt.contentType, got, tt.want)
			}
		})
	}
}

func TestIsJSONContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        bool
	}{
		{"empty", "", false},
		{"application/json", "application/json", true},
		{"application/json with charset", "application/json; charset=utf-8", true},
		{"json variant", "application/vnd.api+json", true},
		{"text/plain", "text/plain", false},
		{"case insensitive", "APPLICATION/JSON", true},
		{"with spaces", "  application/json  ", true},
		{"json variant with params", "application/problem+json; charset=utf-8", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsJSONContentType(tt.contentType); got != tt.want {
				t.Errorf("IsJSONContentType(%q) = %v, want %v", tt.contentType, got, tt.want)
			}
		})
	}
}

func TestCopyHeaders(t *testing.T) {
	tests := []struct {
		name     string
		src      http.Header
		skip     []string
		expected http.Header
	}{
		{
			name: "copy all headers",
			src: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Custom":     []string{"value"},
			},
			skip: nil,
			expected: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Custom":     []string{"value"},
			},
		},
		{
			name: "skip authorization",
			src: http.Header{
				"Content-Type":  []string{"application/json"},
				"Authorization": []string{"Bearer token"},
				"X-Custom":      []string{"value"},
			},
			skip: []string{"authorization"},
			expected: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Custom":     []string{"value"},
			},
		},
		{
			name: "skip multiple headers",
			src: http.Header{
				"Content-Type":  []string{"application/json"},
				"Authorization": []string{"Bearer token"},
				"X-Custom":      []string{"value"},
				"Env":           []string{"production"},
			},
			skip: []string{"authorization", "env"},
			expected: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Custom":     []string{"value"},
			},
		},
		{
			name: "case insensitive skip",
			src: http.Header{
				"Content-Type":  []string{"application/json"},
				"Authorization": []string{"Bearer token"},
			},
			skip: []string{"AUTHORIZATION"},
			expected: http.Header{
				"Content-Type": []string{"application/json"},
			},
		},
		{
			name: "multiple values for same header",
			src: http.Header{
				"X-Custom": []string{"value1", "value2"},
			},
			skip: nil,
			expected: http.Header{
				"X-Custom": []string{"value1", "value2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := make(http.Header)
			CopyHeaders(dst, tt.src, tt.skip...)

			for key, expectedValues := range tt.expected {
				gotValues := dst[key]
				if len(gotValues) != len(expectedValues) {
					t.Errorf("Header %q: got %d values, want %d", key, len(gotValues), len(expectedValues))
					continue
				}
				for i, expectedValue := range expectedValues {
					if gotValues[i] != expectedValue {
						t.Errorf("Header %q[%d]: got %q, want %q", key, i, gotValues[i], expectedValue)
					}
				}
			}

			for key := range dst {
				if _, ok := tt.expected[key]; !ok {
					t.Errorf("Unexpected header %q in result", key)
				}
			}
		})
	}
}

func TestCopyHeadersPreservesMultipleValues(t *testing.T) {
	src := http.Header{
		"Set-Cookie": []string{"cookie1=value1", "cookie2=value2", "cookie3=value3"},
	}
	dst := make(http.Header)

	CopyHeaders(dst, src)

	if len(dst["Set-Cookie"]) != 3 {
		t.Errorf("Expected 3 Set-Cookie values, got %d", len(dst["Set-Cookie"]))
	}

	for i, expected := range []string{"cookie1=value1", "cookie2=value2", "cookie3=value3"} {
		if dst["Set-Cookie"][i] != expected {
			t.Errorf("Set-Cookie[%d]: got %q, want %q", i, dst["Set-Cookie"][i], expected)
		}
	}
}

func TestCopyHeadersWithEmptySkipList(t *testing.T) {
	src := http.Header{
		"Content-Type": []string{"application/json"},
		"X-Custom":     []string{"value"},
	}
	dst := make(http.Header)

	CopyHeaders(dst, src, []string{}...)

	if len(dst) != 2 {
		t.Errorf("Expected 2 headers, got %d", len(dst))
	}
}

func BenchmarkTruncateBytes(b *testing.B) {
	input := []byte(strings.Repeat("a", 20000))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = TruncateBytes(input, BodyTruncationLimit)
	}
}

func BenchmarkCopyHeaders(b *testing.B) {
	src := http.Header{
		"Content-Type":  []string{"application/json"},
		"Authorization": []string{"Bearer token"},
		"X-Custom-1":    []string{"value1"},
		"X-Custom-2":    []string{"value2"},
		"X-Custom-3":    []string{"value3"},
	}
	skip := []string{"authorization"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst := make(http.Header)
		CopyHeaders(dst, src, skip...)
	}
}
