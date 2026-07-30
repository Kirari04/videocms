package controllers

import (
	"ch/kirari04/videocms/models"
	"strings"
	"testing"
)

func TestResolveMP4DownloadRequiresExactlyOneReadyAudio(t *testing.T) {
	link := downloadSelectionTestLink()

	for _, audioUUIDs := range [][]string{
		nil,
		{"audio-one", "audio-two"},
		{"audio-pending"},
		{"audio-unknown"},
	} {
		if _, err := resolveDownloadSelection(
			link,
			"720p",
			downloadContainerMP4,
			false,
			true,
			audioUUIDs,
			nil,
		); err == nil {
			t.Fatalf("resolveDownloadSelection() accepted MP4 audio selection %v", audioUUIDs)
		}
	}

	selection, err := resolveDownloadSelection(
		link,
		"720p",
		downloadContainerMP4,
		false,
		true,
		[]string{"audio-two"},
		nil,
	)
	if err != nil {
		t.Fatalf("resolveDownloadSelection() error = %v", err)
	}
	if len(selection.Audios) != 1 || selection.Audios[0].UUID != "audio-two" {
		t.Fatalf("selected audios = %#v", selection.Audios)
	}
}

func TestResolveMP4DownloadRejectsSubtitles(t *testing.T) {
	link := downloadSelectionTestLink()
	_, err := resolveDownloadSelection(
		link,
		"720p",
		downloadContainerMP4,
		false,
		true,
		[]string{"audio-one"},
		[]string{"subtitle-ass"},
	)
	if err == nil || !strings.Contains(err.Error(), "do not support subtitle") {
		t.Fatalf("resolveDownloadSelection() error = %v", err)
	}
}

func TestResolveMKVDownloadUsesExplicitTrackSelection(t *testing.T) {
	link := downloadSelectionTestLink()
	selection, err := resolveDownloadSelection(
		link,
		"720p",
		downloadContainerMKV,
		false,
		true,
		[]string{"audio-two"},
		[]string{"subtitle-vtt"},
	)
	if err != nil {
		t.Fatalf("resolveDownloadSelection() error = %v", err)
	}
	if len(selection.Audios) != 1 || selection.Audios[0].UUID != "audio-two" {
		t.Fatalf("selected audios = %#v", selection.Audios)
	}
	if len(selection.Subtitles) != 1 || selection.Subtitles[0].UUID != "subtitle-vtt" {
		t.Fatalf("selected subtitles = %#v", selection.Subtitles)
	}
}

func TestResolveDownloadRejectsMissingExplicitSelection(t *testing.T) {
	link := downloadSelectionTestLink()
	_, err := resolveDownloadSelection(
		link,
		"720p",
		downloadContainerMKV,
		false,
		false,
		nil,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "selection is required") {
		t.Fatalf("resolveDownloadSelection() error = %v", err)
	}
}

func TestResolveDownloadRejectsUnavailableQuality(t *testing.T) {
	link := downloadSelectionTestLink()
	for _, quality := range []string{"1080p", "480p"} {
		if _, err := resolveDownloadSelection(
			link,
			quality,
			downloadContainerMKV,
			false,
			false,
			nil,
			nil,
		); err == nil {
			t.Fatalf("resolveDownloadSelection() accepted quality %q", quality)
		}
	}
}

func TestDownloadFFmpegArgsMapSelectedStreams(t *testing.T) {
	link := downloadSelectionTestLink()
	selection, err := resolveDownloadSelection(
		link,
		"720p",
		downloadContainerMKV,
		false,
		true,
		[]string{"audio-two"},
		[]string{"subtitle-ass"},
	)
	if err != nil {
		t.Fatalf("resolveDownloadSelection() error = %v", err)
	}

	args := downloadFFmpegArgs(selection, "/tmp/output.mkv")
	joined := strings.Join(args, " ")
	for _, expected := range []string{
		"-i /quality/720p/video.m3u8",
		"-i /audio/two/audio.m3u8",
		"-i /subtitle/ass/out.ass",
		"-map 0:v:0",
		"-map 1:a:0",
		"-map 2:s:0",
		"-f matroska",
		"/tmp/output.mkv",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("downloadFFmpegArgs() missing %q in %q", expected, joined)
		}
	}
}

func TestDownloadPageOptionsSortQualityAndPreferASSSubtitles(t *testing.T) {
	link := downloadSelectionTestLink()
	qualities := downloadQualityOptions(link.File.Qualitys)
	if len(qualities) != 1 || qualities[0].Name != "720p" {
		t.Fatalf("downloadQualityOptions() = %#v", qualities)
	}

	subtitles := downloadSubtitleOptions(link.File.Subtitles)
	if len(subtitles) != 1 || subtitles[0].UUID != "subtitle-ass" {
		t.Fatalf("downloadSubtitleOptions() = %#v", subtitles)
	}
}

func TestFormatDownloadDuration(t *testing.T) {
	tests := map[string]struct {
		seconds float64
		want    string
	}{
		"unknown":      {seconds: 0, want: ""},
		"under minute": {seconds: 49, want: "< 1m"},
		"minutes":      {seconds: 125, want: "2m"},
		"hours":        {seconds: 3720, want: "1h 02m"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := formatDownloadDuration(test.seconds); got != test.want {
				t.Fatalf("formatDownloadDuration(%v) = %q, want %q", test.seconds, got, test.want)
			}
		})
	}
}

func downloadSelectionTestLink() *models.Link {
	return &models.Link{
		UUID: "link-uuid",
		File: models.File{
			Qualitys: []models.Quality{
				{
					Name:       "720p",
					Type:       "hls",
					Height:     720,
					Width:      1280,
					Path:       "/quality/720p",
					OutputFile: "video.m3u8",
					Ready:      true,
				},
				{
					Name:       "480p",
					Type:       "hls",
					Height:     480,
					Width:      854,
					Path:       "/quality/480p",
					OutputFile: "video.m3u8",
					Ready:      false,
				},
			},
			Audios: []models.Audio{
				{
					UUID:       "audio-one",
					Name:       "English",
					Lang:       "eng",
					Index:      0,
					Path:       "/audio/one",
					OutputFile: "audio.m3u8",
					Ready:      true,
				},
				{
					UUID:       "audio-two",
					Name:       "Commentary",
					Lang:       "eng",
					Index:      1,
					Path:       "/audio/two",
					OutputFile: "audio.m3u8",
					Ready:      true,
				},
				{
					UUID:  "audio-pending",
					Name:  "Pending",
					Index: 2,
					Ready: false,
				},
			},
			Subtitles: []models.Subtitle{
				{
					UUID:       "subtitle-vtt",
					Name:       "English",
					Lang:       "eng",
					Type:       "vtt",
					Index:      0,
					Path:       "/subtitle/vtt",
					OutputFile: "out.vtt",
					Ready:      true,
				},
				{
					UUID:       "subtitle-ass",
					Name:       "English",
					Lang:       "eng",
					Type:       "ass",
					Index:      0,
					Path:       "/subtitle/ass",
					OutputFile: "out.ass",
					Ready:      true,
				},
				{
					UUID:  "subtitle-pending",
					Name:  "Pending",
					Type:  "ass",
					Index: 1,
					Ready: false,
				},
			},
		},
	}
}
