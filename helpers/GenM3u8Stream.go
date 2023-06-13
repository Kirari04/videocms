package helpers

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/models"
	"fmt"
)

func GenM3u8Stream(dbLink *models.Link, qualitys *[]models.Quality, audio *models.Audio) string {
	m3u8 := "#EXTM3U\n#EXT-X-VERSION:6"
	if audio != nil {
		m3u8 += fmt.Sprintf(
			"\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"AAC\",NAME=\"Subtitle\",LANGUAGE=\"%s\",URI=\"%s\"",
			audio.Lang,
			fmt.Sprintf("%s/%s/%s/audio/%s", config.ENV.FolderVideoQualitysPub, dbLink.UUID, audio.UUID, audio.OutputFile),
		)
	}
	for _, quality := range *qualitys {
		if quality.Type == "hls" && quality.Ready {
			m3u8 += fmt.Sprintf(
				"\n#EXT-X-STREAM-INF:BANDWIDTH=%d,AUDIO=\"AAC\",RESOLUTION=%s,CODECS=\"avc1.640015,mp4a.40.2\"\n%s",
				int64(quality.Height*quality.Width*2),
				fmt.Sprintf("%dx%d", quality.Height, quality.Height),
				fmt.Sprintf("%s/%s/%s/%s", config.ENV.FolderVideoQualitysPub, dbLink.UUID, quality.Name, quality.OutputFile),
			)
		}

	}
	return m3u8
}
