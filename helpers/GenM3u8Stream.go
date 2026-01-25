package helpers

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/models"
	"fmt"
)

func GenM3u8Stream(dbLink *models.Link, qualitys *[]models.Quality, audio *models.Audio, JWT string) string {
	m3u8 := "#EXTM3U\n#EXT-X-VERSION:6"
	if audio != nil {
		m3u8 += fmt.Sprintf(
			"\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"AAC\",NAME=\"Subtitle\",LANGUAGE=\"%s\",URI=\"%s\"",
			audio.Lang,
			fmt.Sprintf("%s/%s/%s/audio/%s?jwt=%s", config.ENV.FolderVideoQualitysPub, dbLink.UUID, audio.UUID, audio.OutputFile, JWT),
		)
	}
	for _, quality := range *qualitys {
		if quality.Type == "hls" && quality.Ready {
			m3u8 += fmt.Sprintf(
				"\n#EXT-X-STREAM-INF:BANDWIDTH=%d,AUDIO=\"AAC\",RESOLUTION=%s,CODECS=\"avc1.640015,mp4a.40.2\"\n%s",
				int64(quality.Height*quality.Width*2),
				fmt.Sprintf("%dx%d", quality.Width, quality.Height),
				fmt.Sprintf("%s/%s/%s/%s?jwt=%s", config.ENV.FolderVideoQualitysPub, dbLink.UUID, quality.Name, quality.OutputFile, JWT),
			)
		}

	}
	return m3u8
}

func GenM3u8StreamMulti(dbLink *models.Link, qualitys *[]models.Quality, audios *[]models.Audio, JWT string) string {
	m3u8 := "#EXTM3U\n#EXT-X-VERSION:6"

	// Check if there are any audios, if so, we use the "AAC" group (matching existing convention, though "audio" might be better, sticking to convention for safety)
	// Actually, the previous code used "AAC" as GroupID.
	audioGroupID := "AAC"

	hasAudio := false
	if audios != nil && len(*audios) > 0 {
		for i, audio := range *audios {
			if audio.Ready && audio.Type == "hls" {
				hasAudio = true
				isDefault := "NO"
				if i == 0 {
					isDefault = "YES"
				}
				// Using audio.Name or audio.Lang for NAME.
				m3u8 += fmt.Sprintf(
					"\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"%s\",NAME=\"%s\",LANGUAGE=\"%s\",DEFAULT=%s,AUTOSELECT=YES,URI=\"%s\"",
					audioGroupID,
					audio.Name, // Using Name instead of "Subtitle" as in the original single-audio function which seemed weird
					audio.Lang,
					isDefault,
					fmt.Sprintf("%s/%s/%s/audio/%s?jwt=%s", config.ENV.FolderVideoQualitysPub, dbLink.UUID, audio.UUID, audio.OutputFile, JWT),
				)
			}
		}
	}

	for _, quality := range *qualitys {
		if quality.Type == "hls" && quality.Ready {
			audioTag := ""
			if hasAudio {
				audioTag = fmt.Sprintf(",AUDIO=\"%s\"", audioGroupID)
			}
			m3u8 += fmt.Sprintf(
				"\n#EXT-X-STREAM-INF:BANDWIDTH=%d%s,RESOLUTION=%s,CODECS=\"avc1.640015,mp4a.40.2\"\n%s",
				int64(quality.Height*quality.Width*2),
				audioTag,
				fmt.Sprintf("%dx%d", quality.Width, quality.Height),
				fmt.Sprintf("%s/%s/%s/%s?jwt=%s", config.ENV.FolderVideoQualitysPub, dbLink.UUID, quality.Name, quality.OutputFile, JWT),
			)
		}

	}
	return m3u8
}
