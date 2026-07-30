package main

import (
	"html/template"
	"os"
	"strings"
	"testing"
)

func TestMediaTemplatesParse(t *testing.T) {
	if _, err := template.ParseGlob("views/*.html"); err != nil {
		t.Fatalf("template.ParseGlob() error = %v", err)
	}
}

func TestPlayerTemplatesDoNotRenderQueryJWT(t *testing.T) {
	for _, path := range []string{"views/player.html", "views/player_v2.html", "views/player.old.html"} {
		content := readTemplateFile(t, path)
		if strings.Contains(string(content), "?jwt=") {
			t.Fatalf("%s still contains query jwt", path)
		}
	}
}

func TestPlayerTemplatesRenderEncodingState(t *testing.T) {
	for _, path := range []string{"views/player.html", "views/player_v2.html"} {
		content := string(readTemplateFile(t, path))
		for _, expected := range []string{
			"Video is still being encoded",
			"data-encoding-state",
			"startEncodingStatusPolling",
			"/v/${UUID}/status",
		} {
			if !strings.Contains(content, expected) {
				t.Fatalf("%s missing encoding state marker %q", path, expected)
			}
		}
	}
}

func TestPlayerV2TemplateUsesViewportConstrainedAspectRatio(t *testing.T) {
	content := string(readTemplateFile(t, "views/player_v2.html"))
	for _, expected := range []string{
		"media-player[data-media-player]",
		"[data-media-provider]",
		"[data-media-provider] video",
		"<media-poster class=\"vds-poster\" src=\"{{ .Thumbnail }}\" alt=\"{{ .Title }}\"></media-poster>",
		"width: 100%",
		"height: 100%",
		"aspect-ratio: auto",
		"object-fit: contain",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("player_v2.html missing viewport sizing marker %q", expected)
		}
	}
}

func TestPlayerTemplatesOpenDownloadOptionsPage(t *testing.T) {
	for _, path := range []string{"views/player.html", "views/player_v2.html"} {
		content := string(readTemplateFile(t, path))
		for _, expected := range []string{
			"DOWNLOAD_PAGE_URL",
			"`/v/${UUID}/download`",
		} {
			if !strings.Contains(content, expected) {
				t.Fatalf("%s missing download page marker %q", path, expected)
			}
		}
	}
}

func TestPlayerV2UsesAbsoluteDownloadURLForVidstack(t *testing.T) {
	content := string(readTemplateFile(t, "views/player_v2.html"))
	expected := "new URL(`/v/${UUID}/download`, window.location.origin).href"
	if !strings.Contains(content, expected) {
		t.Fatalf("player_v2.html must pass Vidstack an absolute download URL")
	}
}

func TestDownloadTemplateContainsSelectionRules(t *testing.T) {
	content := string(readTemplateFile(t, "views/download.html"))
	for _, expected := range []string{
		`name="quality"`,
		`name="container"`,
		`value="mkv"`,
		`value="mp4"`,
		`name="audio"`,
		`name="subtitle"`,
		"MP4 requires exactly one audio track",
		"Download manifest",
		`class="brand-logo"`,
		`src="/logo.png"`,
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("download.html missing selection marker %q", expected)
		}
	}
}

func readTemplateFile(t *testing.T, path string) []byte {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return content
}
