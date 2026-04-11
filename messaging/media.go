package messaging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ringclaw/ringclaw/ringcentral"
)

var mediaHTTPClient = &http.Client{
	Timeout: 60 * time.Second,
	Transport: &http.Transport{
		// Custom dialer blocks connections to private/reserved IP ranges (SSRF protection).
		DialContext: safeDialContext,
	},
}

// safeDialContext validates that the target address is not a private/reserved IP
// before establishing a connection.
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address %q: %w", addr, err)
	}
	if err := checkHost(ctx, host); err != nil {
		return nil, err
	}
	return (&net.Dialer{}).DialContext(ctx, network, addr)
}

// checkHost resolves the host (if it's a hostname) and verifies none of the
// resulting IPs are private/reserved.
func checkHost(ctx context.Context, host string) error {
	ip := net.ParseIP(host)
	if ip != nil {
		if isPrivateIP(ip) {
			return fmt.Errorf("private IP %s is blocked for security", ip)
		}
		return nil
	}
	// Hostname — resolve and check all IPs.
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", host, err)
	}
	for _, resolved := range ips {
		if isPrivateIP(resolved.IP) {
			return fmt.Errorf("host %q resolves to private IP %s — blocked for security", host, resolved.IP)
		}
	}
	return nil
}

// privateCIDRs lists network ranges that should not be accessed by the media downloader.
var privateCIDRs = []*net.IPNet{
	parseCIDR("10.0.0.0/8"),
	parseCIDR("172.16.0.0/12"),
	parseCIDR("192.168.0.0/16"),
	parseCIDR("169.254.0.0/16"),
	parseCIDR("fc00::/7"),
}

// isPrivateIP reports whether an IP address is private, loopback, link-local,
// or otherwise should not be accessed from a server-side HTTP client.
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip.IsUnspecified() {
		return true
	}
	// IPv4 private ranges: 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16
	for _, cidr := range privateCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

func parseCIDR(s string) *net.IPNet {
	_, network, err := net.ParseCIDR(s)
	if err != nil {
		panic(fmt.Sprintf("invalid CIDR %q: %v", s, err))
	}
	return network
}

// reMarkdownImage matches markdown image syntax: ![alt](url)
var reMarkdownImage = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)

// ExtractImageURLs extracts image URLs from markdown text.
func ExtractImageURLs(text string) []string {
	matches := reMarkdownImage.FindAllStringSubmatch(text, -1)
	var urls []string
	for _, m := range matches {
		url := strings.TrimSpace(m[1])
		if strings.HasPrefix(url, "https://") {
			urls = append(urls, url)
		}
	}
	return urls
}

// SendMediaFromURL downloads a file from a URL and uploads it to a RingCentral chat.
func SendMediaFromURL(ctx context.Context, client *ringcentral.Client, chatID, mediaURL string) error {
	data, _, err := downloadFile(ctx, mediaURL)
	if err != nil {
		return fmt.Errorf("download %s: %w", mediaURL, err)
	}

	fileName := filenameFromURL(mediaURL)
	slog.Info("uploading file", "component", "media", "fileName", fileName, "bytes", len(data), "chatID", chatID)

	_, err = client.UploadFile(ctx, chatID, fileName, data)
	if err != nil {
		return fmt.Errorf("upload file: %w", err)
	}

	slog.Info("sent file", "component", "media", "fileName", fileName, "chatID", chatID)
	return nil
}

func downloadFile(ctx context.Context, url string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}

	resp, err := mediaHTTPClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	contentType := resp.Header.Get("Content-Type")
	return data, contentType, nil
}

func filenameFromURL(rawURL string) string {
	u := stripQuery(rawURL)
	name := filepath.Base(u)
	if name == "" || name == "." || name == "/" {
		return "file"
	}
	return name
}

func stripQuery(rawURL string) string {
	if i := strings.IndexByte(rawURL, '?'); i >= 0 {
		return rawURL[:i]
	}
	return rawURL
}
