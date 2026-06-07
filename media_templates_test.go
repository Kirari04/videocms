package main

import (
	"os"
	"strings"
	"testing"
)

func TestPlayerTemplatesDoNotRenderQueryJWT(t *testing.T) {
	for _, path := range []string{"views/player.html", "views/player_v2.html", "views/player.old.html"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		if strings.Contains(string(content), "?jwt=") {
			t.Fatalf("%s still contains query jwt", path)
		}
	}
}
