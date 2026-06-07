package main

import (
	"os"
	"strings"
	"testing"
)

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

func readTemplateFile(t *testing.T, path string) []byte {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return content
}
