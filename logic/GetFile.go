package logic

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

type GetFileRespQuali struct {
	Name         string
	Type         string
	Height       int64
	Width        int64
	AvgFrameRate float64
	Ready        bool
	Failed       bool
	Progress     float64
	Size         int64
}
type GetFileRespSub struct {
	Name  string
	Type  string
	Lang  string
	Ready bool
}
type GetFileRespAudio struct {
	Name  string
	Type  string
	Lang  string
	Ready bool
}
type GetFileResp struct {
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ID             uint
	UUID           string
	Name           string
	Thumbnail      string
	ParentFolderID uint
	Size           int64
	Duration       float64
	Qualitys       []GetFileRespQuali
	Subtitles      []GetFileRespSub
	Audios         []GetFileRespAudio
}

func GetFile(LinkID uint, userID uint) (status int, fileData *GetFileResp, err error) {

	// query all files
	var link models.Link
	if res := inits.DB.
		Model(&models.Link{}).
		Preload("User").
		Preload("File").
		Preload("File.Qualitys").
		Preload("File.Subtitles").
		Preload("File.Audios").
		Where(&models.Link{
			UserID: userID,
		}).
		First(&link, LinkID); res.Error != nil {
		return http.StatusNotFound, nil, echo.ErrNotFound
	}

	var Qualitys []GetFileRespQuali
	for _, Quality := range link.File.Qualitys {
		avgFps := Quality.AvgFrameRate
		if avgFps == 0 {
			avgFps = link.File.AvgFrameRate
		}
		Qualitys = append(Qualitys, GetFileRespQuali{
			Name:         Quality.Name,
			Type:         Quality.Type,
			Height:       Quality.Height,
			Width:        Quality.Width,
			Size:         Quality.Size,
			AvgFrameRate: avgFps,
			Ready:        Quality.Ready,
			Progress:     Quality.Progress * 100,
			Failed:       Quality.Failed,
		})
	}

	var Subtitles []GetFileRespSub
	for _, Subtitle := range link.File.Subtitles {
		Subtitles = append(Subtitles, GetFileRespSub{
			Name:  Subtitle.Name,
			Lang:  Subtitle.Lang,
			Type:  Subtitle.Type,
			Ready: Subtitle.Ready,
		})
	}

	var Audios []GetFileRespAudio
	for _, Audio := range link.File.Audios {
		Audios = append(Audios, GetFileRespAudio{
			Name:  Audio.Name,
			Lang:  Audio.Lang,
			Type:  Audio.Type,
			Ready: Audio.Ready,
		})
	}

	response := GetFileResp{
		CreatedAt:      *link.CreatedAt,
		UpdatedAt:      *link.UpdatedAt,
		ID:             link.ID,
		UUID:           link.UUID,
		Name:           link.Name,
		Thumbnail:      fmt.Sprintf("%s/%s/image/thumb/%s", config.ENV.FolderVideoQualitysPub, link.UUID, link.File.Thumbnail),
		ParentFolderID: link.ParentFolderID,
		Size:           link.File.Size,
		Duration:       link.File.Duration,
		Qualitys:       Qualitys,
		Subtitles:      Subtitles,
		Audios:         Audios,
	}

	return http.StatusOK, &response, nil
}
