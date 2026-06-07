package services

import (
	"net"
	"strings"
	"testing"
)

func TestValidateRemoteURLScheme(t *testing.T) {
	valid := []string{
		"http://example.com/video.mp4",
		"https://example.com/video.mp4",
	}
	for _, rawURL := range valid {
		if err := validateRemoteURLScheme(rawURL); err != nil {
			t.Fatalf("expected %s to be valid: %v", rawURL, err)
		}
	}

	invalid := []string{
		"ftp://example.com/video.mp4",
		"https:///missing-host",
	}
	for _, rawURL := range invalid {
		if err := validateRemoteURLScheme(rawURL); err == nil {
			t.Fatalf("expected %s to be invalid", rawURL)
		}
	}
}

func TestBlockedRemoteIPs(t *testing.T) {
	blocked := []string{
		"127.0.0.1",
		"10.0.0.1",
		"172.16.0.1",
		"192.168.1.1",
		"169.254.1.1",
		"224.0.0.1",
		"::1",
		"fc00::1",
	}
	for _, rawIP := range blocked {
		if !isBlockedRemoteIP(net.ParseIP(rawIP)) {
			t.Fatalf("expected %s to be blocked", rawIP)
		}
	}

	if isBlockedRemoteIP(net.ParseIP("93.184.216.34")) {
		t.Fatal("expected public IP to be allowed")
	}
}

func TestSanitizeRemoteFileName(t *testing.T) {
	name := sanitizeRemoteFileName(`bad<>:"/\|?*.mp4`, 1)
	if strings.ContainsAny(name, `<>:"/\|?*`) {
		t.Fatalf("expected sanitized filename, got %q", name)
	}

	longName := sanitizeRemoteFileName(strings.Repeat("a", 160)+".mp4", 1)
	if len([]rune(longName)) > 128 {
		t.Fatalf("expected filename to be truncated to 128 runes, got %d", len([]rune(longName)))
	}
	if !strings.HasSuffix(longName, ".mp4") {
		t.Fatalf("expected extension to be preserved, got %q", longName)
	}
}
