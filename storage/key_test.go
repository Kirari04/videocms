package storage

import (
	"errors"
	"testing"
)

func TestParseKeyRejectsUnsafeValues(t *testing.T) {
	for _, value := range []string{
		"",
		"/absolute/file",
		"../outside",
		"safe/../outside",
		"safe//file",
		`safe\file`,
		"safe/./file",
	} {
		t.Run(value, func(t *testing.T) {
			if _, err := ParseKey(value); !errors.Is(err, ErrInvalidKey) {
				t.Fatalf("ParseKey(%q) error = %v, want ErrInvalidKey", value, err)
			}
		})
	}
}

func TestLegacyMediaLayoutMatchesExistingTree(t *testing.T) {
	layout := LegacyMediaLayout{}
	source, sourceErr := layout.Source("file", "original.mp4")
	video, videoErr := layout.Video("file", "720p", "out0.ts")
	videoPrefix, videoPrefixErr := layout.VideoPrefix("file", "720p")
	audio, audioErr := layout.Audio("file", "audio", "audio0.ts")
	subtitle, subtitleErr := layout.Subtitle("file", "subtitle", "out.vtt")
	thumbnail, thumbnailErr := layout.Thumbnail("file", "4x4.webp")
	tests := []struct {
		name string
		key  Key
		want string
	}{
		{name: "source", key: mustLayoutKey(t, source, sourceErr), want: "file/source/original.mp4"},
		{name: "video", key: mustLayoutKey(t, video, videoErr), want: "file/720p/out0.ts"},
		{name: "video prefix", key: mustLayoutKey(t, videoPrefix, videoPrefixErr), want: "file/720p"},
		{name: "audio", key: mustLayoutKey(t, audio, audioErr), want: "file/audio/audio0.ts"},
		{name: "subtitle", key: mustLayoutKey(t, subtitle, subtitleErr), want: "file/subtitle/out.vtt"},
		{name: "thumbnail", key: mustLayoutKey(t, thumbnail, thumbnailErr), want: "file/4x4.webp"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.key.String() != test.want {
				t.Fatalf("key = %q, want %q", test.key.String(), test.want)
			}
		})
	}
}

func mustLayoutKey(t *testing.T, key Key, err error) Key {
	t.Helper()
	if err != nil {
		t.Fatalf("layout key error = %v", err)
	}
	return key
}
