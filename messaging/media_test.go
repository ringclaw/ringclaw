package messaging

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractImageURLs(t *testing.T) {
	text := "check ![img](https://example.com/a.png) and ![](https://example.com/b.jpg)"
	urls := ExtractImageURLs(text)
	if len(urls) != 2 {
		t.Fatalf("expected 2 urls, got %d", len(urls))
	}
	if urls[0] != "https://example.com/a.png" {
		t.Errorf("urls[0] = %q", urls[0])
	}
	if urls[1] != "https://example.com/b.jpg" {
		t.Errorf("urls[1] = %q", urls[1])
	}
}

func TestExtractImageURLs_NoImages(t *testing.T) {
	urls := ExtractImageURLs("just plain text")
	if len(urls) != 0 {
		t.Errorf("expected 0 urls, got %d", len(urls))
	}
}

func TestExtractImageURLs_RelativeURL(t *testing.T) {
	text := "![img](./local.png)"
	urls := ExtractImageURLs(text)
	if len(urls) != 0 {
		t.Errorf("expected 0 urls for relative path, got %d", len(urls))
	}
}

func TestFilenameFromURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://example.com/photo.png", "photo.png"},
		{"https://example.com/path/to/report.pdf", "report.pdf"},
		{"https://example.com/file", "file"},
	}
	for _, tt := range tests {
		got := filenameFromURL(tt.url)
		if got != tt.want {
			t.Errorf("filenameFromURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestFilenameFromURL_WithQuery(t *testing.T) {
	got := filenameFromURL("https://example.com/photo.png?token=abc")
	if got != "photo.png" {
		t.Errorf("got %q, want %q", got, "photo.png")
	}
}

func TestStripQuery(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.com/a?b=c", "https://example.com/a"},
		{"https://example.com/a", "https://example.com/a"},
		{"https://example.com/?x=1&y=2", "https://example.com/"},
	}
	for _, tt := range tests {
		got := stripQuery(tt.input)
		if got != tt.want {
			t.Errorf("stripQuery(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExtractImageURLs_HTTPSOnly(t *testing.T) {
	text := "![http](http://example.com/a.png) ![https](https://example.com/b.png)"
	urls := ExtractImageURLs(text)
	if len(urls) != 1 {
		t.Fatalf("expected 1 url (https only), got %d", len(urls))
	}
	if urls[0] != "https://example.com/b.png" {
		t.Errorf("got %q", urls[0])
	}
}

func TestIsPrivateIP(t *testing.T) {
	private := []string{
		"127.0.0.1", "::1",
		"10.0.0.1", "10.255.255.255",
		"172.16.0.1", "172.31.255.255",
		"192.168.0.1", "192.168.1.1",
		"169.254.0.1",
		"0.0.0.0", "::",
		"fc00::1", "fdff::1",
		"fe80::1",
	}
	for _, s := range private {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("invalid IP %q", s)
		}
		if !isPrivateIP(ip) {
			t.Errorf("expected %s to be private", s)
		}
	}

	public := []string{
		"8.8.8.8", "1.1.1.1", "203.0.113.1",
		"2001:4860:4860::8888",
	}
	for _, s := range public {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("invalid IP %q", s)
		}
		if isPrivateIP(ip) {
			t.Errorf("expected %s to be public", s)
		}
	}
}

func TestResolveAndValidate_PublicIP(t *testing.T) {
	ip, err := resolveAndValidate(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip != "8.8.8.8" {
		t.Errorf("got %q, want 8.8.8.8", ip)
	}
}

func TestResolveAndValidate_PrivateIP(t *testing.T) {
	_, err := resolveAndValidate(context.Background(), "10.0.0.1")
	if err == nil {
		t.Error("expected error for private IP")
	}
}

func TestResolveAndValidate_LoopbackIP(t *testing.T) {
	_, err := resolveAndValidate(context.Background(), "127.0.0.1")
	if err == nil {
		t.Error("expected error for loopback IP")
	}
}

func TestResolveAndValidate_IPv6Loopback(t *testing.T) {
	_, err := resolveAndValidate(context.Background(), "::1")
	if err == nil {
		t.Error("expected error for IPv6 loopback")
	}
}

func TestResolveAndValidate_UnspecifiedIP(t *testing.T) {
	_, err := resolveAndValidate(context.Background(), "0.0.0.0")
	if err == nil {
		t.Error("expected error for unspecified IP")
	}
}

func TestResolveAndValidate_Hostname(t *testing.T) {
	// Use a well-known public hostname
	ip, err := resolveAndValidate(context.Background(), "dns.google")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		t.Errorf("returned %q is not a valid IP", ip)
	}
}

func TestResolveAndValidate_UnresolvableHost(t *testing.T) {
	_, err := resolveAndValidate(context.Background(), "this-host-does-not-exist-xyz.invalid")
	if err == nil {
		t.Error("expected error for unresolvable host")
	}
}

func TestValidateMediaURL_ValidHTTPS(t *testing.T) {
	url, err := validateMediaURL(context.Background(), "https://dns.google/test.png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url == "" {
		t.Error("expected non-empty URL")
	}
}

func TestValidateMediaURL_HTTPRejected(t *testing.T) {
	_, err := validateMediaURL(context.Background(), "http://example.com/test.png")
	if err == nil {
		t.Error("expected error for http:// URL")
	}
}

func TestValidateMediaURL_InvalidURL(t *testing.T) {
	_, err := validateMediaURL(context.Background(), "://bad")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestValidateMediaURL_PrivateHost(t *testing.T) {
	_, err := validateMediaURL(context.Background(), "https://10.0.0.1/test.png")
	if err == nil {
		t.Error("expected error for private host")
	}
}

func TestSafeDialContext_PrivateBlocked(t *testing.T) {
	_, err := safeDialContext(context.Background(), "tcp", "127.0.0.1:443")
	if err == nil {
		t.Error("expected error for private IP dial")
	}
}

func TestSafeDialContext_InvalidAddr(t *testing.T) {
	_, err := safeDialContext(context.Background(), "tcp", "no-port")
	if err == nil {
		t.Error("expected error for address without port")
	}
}

func TestDownloadFile_BlocksPrivateIP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("should not reach"))
	}))
	defer srv.Close()

	// The test server listens on 127.0.0.1 — a private IP
	_, _, err := downloadFile(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error for private IP, got nil")
	}
}

func TestDownloadFile_PublicServer(t *testing.T) {
	// Test that the function correctly reports an error for a nonexistent host
	_, _, err := downloadFile(context.Background(), "https://invalid.example.local/nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent host")
	}
}
