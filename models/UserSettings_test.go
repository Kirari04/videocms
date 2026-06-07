package models

import "testing"

func TestUserSettingsEffectiveRemoteDownloadEnabledDefaultsTrue(t *testing.T) {
	settings := UserSettings{}

	if !settings.EffectiveRemoteDownloadEnabled() {
		t.Fatal("missing RemoteDownloadEnabled should default to enabled")
	}
}

func TestUserSettingsEffectiveRemoteDownloadEnabledUsesExplicitValue(t *testing.T) {
	enabled := false
	settings := UserSettings{RemoteDownloadEnabled: &enabled}

	if settings.EffectiveRemoteDownloadEnabled() {
		t.Fatal("explicit disabled RemoteDownloadEnabled should be respected")
	}
}

func TestUserSettingsEffectiveMaxRemoteDownloadsDefaults(t *testing.T) {
	settings := UserSettings{}

	if got := settings.EffectiveMaxRemoteDownloads(); got != DefaultMaxRemoteDownloads {
		t.Fatalf("expected default max remote downloads %d, got %d", DefaultMaxRemoteDownloads, got)
	}
}

func TestUserSettingsEffectiveMaxRemoteDownloadsUsesExplicitValue(t *testing.T) {
	settings := UserSettings{MaxRemoteDownloads: 12}

	if got := settings.EffectiveMaxRemoteDownloads(); got != 12 {
		t.Fatalf("expected explicit max remote downloads 12, got %d", got)
	}
}
