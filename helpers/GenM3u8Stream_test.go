package helpers

import (
	"ch/kirari04/videocms/models"
	"strings"
	"testing"
)

func TestGenM3u8StreamDoesNotEmitQueryJWT(t *testing.T) {
	link := models.Link{UUID: "link-uuid"}
	qualities := []models.Quality{{Name: "720p", Width: 1280, Height: 720, Type: "hls", Ready: true, OutputFile: "out.m3u8"}}
	audio := models.Audio{UUID: "audio-uuid", Lang: "en", OutputFile: "audio.m3u8"}

	playlist := GenM3u8Stream("/videos/qualitys", &link, &qualities, &audio)
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

func TestPlaylistsKeepMetadataInsideQuotedAttributes(t *testing.T) {
	link := models.Link{UUID: "link-uuid"}
	qualities := []models.Quality{{Name: "720p", Width: 1280, Height: 720, Type: "hls", Ready: true, OutputFile: "out.m3u8"}}
	for _, value := range []string{"en\"\n#EXT-X-STREAM-INF:BANDWIDTH=1\nhttps://attacker.example/payload", "en\rURI=\"evil\"", "en\x00", strings.Repeat("a", 1000)} {
		audio := models.Audio{UUID: "audio-uuid", Name: value, Lang: value, OutputFile: "audio.m3u8", Type: "hls", Ready: true}
		for _, playlist := range []string{GenM3u8Stream("/media", &link, &qualities, &audio), GenM3u8StreamMulti("/media", &link, &qualities, &[]models.Audio{audio})} {
			if strings.Contains(playlist, "attacker") || strings.Contains(playlist, "evil") || strings.ContainsAny(playlist, "\r\x00") || strings.Count(playlist, "#EXT-X-STREAM-INF:") != 1 || len(strings.Split(playlist, "\n")) != 5 {
				t.Fatalf("metadata escaped attribute boundary: %q", playlist)
			}
		}
	}
	audios := []models.Audio{{UUID: "audio-uuid", Name: "日本語, commentary", Lang: "ja", Type: "hls", Ready: true, OutputFile: "audio.m3u8"}}
	playlist := GenM3u8StreamMulti("/media", &link, &qualities, &audios)
	if !strings.Contains(playlist, `NAME="日本語, commentary",LANGUAGE="ja"`) {
		t.Fatal("legitimate Unicode/comma label was changed")
	}
}
