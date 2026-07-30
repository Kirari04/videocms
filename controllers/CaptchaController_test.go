package controllers

import "testing"

func TestSafeCaptchaReturn(t *testing.T) {
	if got := safeCaptchaReturn("download"); got != "download" {
		t.Fatalf("safeCaptchaReturn(download) = %q", got)
	}
	for _, unsafe := range []string{"https://example.com", "//example.com", "../admin", "player"} {
		if got := safeCaptchaReturn(unsafe); got != "" {
			t.Fatalf("safeCaptchaReturn(%q) = %q", unsafe, got)
		}
	}
}
