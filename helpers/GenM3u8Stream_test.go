package helpers

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/models"
	"strings"
	"testing"
)

func TestGenM3u8StreamDoesNotEmitQueryJWT(t *testing.T) {
	previousFolder := config.ENV.FolderVideoQualitysPub
	config.ENV.FolderVideoQualitysPub = "/videos/qualitys"
	t.Cleanup(func() {
		config.ENV.FolderVideoQualitysPub = previousFolder
	})

	link := models.Link{UUID: "link-uuid"}
	qualities := []models.Quality{{Name: "720p", Width: 1280, Height: 720, Type: "hls", Ready: true, OutputFile: "out.m3u8"}}
	audio := models.Audio{UUID: "audio-uuid", Lang: "en", OutputFile: "audio.m3u8"}

	playlist := GenM3u8Stream(&link, &qualities, &audio)
	if strings.Contains(playlist, "?jwt=") {
		t.Fatalf("playlist contains query jwt: %s", playlist)
	}
	if !strings.Contains(playlist, "/videos/qualitys/link-uuid/720p/out.m3u8") {
		t.Fatalf("playlist missing tokenless video URL: %s", playlist)
	}
	if !strings.Contains(playlist, "/videos/qualitys/link-uuid/audio-uuid/audio/audio.m3u8") {
		t.Fatalf("playlist missing tokenless audio URL: %s", playlist)
	}
}
