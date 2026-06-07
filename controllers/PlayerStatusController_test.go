package controllers

import (
	"ch/kirari04/videocms/models"
	"testing"
)

func TestBuildPlayerStatusQueuedQuality(t *testing.T) {
	status := BuildPlayerStatus(playerStatusLink(
		[]models.Quality{
			{Name: "720p", Type: "hls"},
		},
		nil,
		nil,
	))

	if status.Ready {
		t.Fatal("expected queued quality to be unready")
	}
	if status.State != PlayerStateQueued {
		t.Fatalf("state = %q, want %q", status.State, PlayerStateQueued)
	}
	if status.TotalQualityCount != 1 || status.ReadyQualityCount != 0 {
		t.Fatalf("quality counts = %d/%d, want 0/1 ready/total", status.ReadyQualityCount, status.TotalQualityCount)
	}
}

func TestBuildPlayerStatusEncodingQuality(t *testing.T) {
	status := BuildPlayerStatus(playerStatusLink(
		[]models.Quality{
			{Name: "720p", Type: "hls", Encoding: true, Progress: 0.42},
		},
		nil,
		nil,
	))

	if status.State != PlayerStateEncoding {
		t.Fatalf("state = %q, want %q", status.State, PlayerStateEncoding)
	}
	if status.PlaybackProgress != 42 {
		t.Fatalf("playback progress = %v, want 42", status.PlaybackProgress)
	}
	if status.ActiveTaskProgress != 42 {
		t.Fatalf("active task progress = %v, want 42", status.ActiveTaskProgress)
	}
}

func TestBuildPlayerStatusEncodingAudioBeforeQuality(t *testing.T) {
	status := BuildPlayerStatus(playerStatusLink(
		[]models.Quality{
			{Name: "720p", Type: "hls"},
		},
		[]models.Audio{
			{Name: "English", Encoding: true, Progress: 0.71},
		},
		nil,
	))

	if status.State != PlayerStateEncoding {
		t.Fatalf("state = %q, want %q", status.State, PlayerStateEncoding)
	}
	if status.PlaybackProgress != 0 {
		t.Fatalf("playback progress = %v, want 0", status.PlaybackProgress)
	}
	if status.ActiveTaskProgress != 71 {
		t.Fatalf("active task progress = %v, want 71", status.ActiveTaskProgress)
	}
}

func TestBuildPlayerStatusReadyQuality(t *testing.T) {
	status := BuildPlayerStatus(playerStatusLink(
		[]models.Quality{
			{Name: "720p", Type: "hls", Ready: true},
		},
		nil,
		nil,
	))

	if !status.Ready {
		t.Fatal("expected ready HLS quality to make player ready")
	}
	if status.State != PlayerStateReady {
		t.Fatalf("state = %q, want %q", status.State, PlayerStateReady)
	}
	if status.ReadyQualityCount != 1 {
		t.Fatalf("ready quality count = %d, want 1", status.ReadyQualityCount)
	}
}

func TestBuildPlayerStatusAllQualitiesFailed(t *testing.T) {
	status := BuildPlayerStatus(playerStatusLink(
		[]models.Quality{
			{Name: "360p", Type: "hls", Failed: true},
			{Name: "720p", Type: "hls", Failed: true},
		},
		nil,
		nil,
	))

	if status.Ready {
		t.Fatal("expected failed qualities to be unready")
	}
	if status.State != PlayerStateFailed {
		t.Fatalf("state = %q, want %q", status.State, PlayerStateFailed)
	}
}

func TestBuildPlayerStatusNonHLSReadyQualityIsNotPlayable(t *testing.T) {
	status := BuildPlayerStatus(playerStatusLink(
		[]models.Quality{
			{Name: "download", Type: "mp4", Ready: true},
		},
		nil,
		nil,
	))

	if status.Ready {
		t.Fatal("expected non-HLS ready quality to be unplayable")
	}
	if status.State != PlayerStateFailed {
		t.Fatalf("state = %q, want %q", status.State, PlayerStateFailed)
	}
	if status.TotalQualityCount != 0 {
		t.Fatalf("total playable quality count = %d, want 0", status.TotalQualityCount)
	}
}

func playerStatusLink(qualitys []models.Quality, audios []models.Audio, subtitles []models.Subtitle) *models.Link {
	return &models.Link{
		UUID: "550e8400-e29b-41d4-a716-446655440000",
		File: models.File{
			Qualitys:  qualitys,
			Audios:    audios,
			Subtitles: subtitles,
		},
	}
}
