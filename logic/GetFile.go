package logic

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
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
		return fiber.StatusNotFound, nil, errors.New("")
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
		UUID:           link.UUID,
		Name:           link.Name,
		Thumbnail:      fmt.Sprintf("/videos/qualitys/%s/image/thumb/%s", link.UUID, link.File.Thumbnail),
		ParentFolderID: link.ParentFolderID,
		Size:           link.File.Size,
		Duration:       link.File.Duration,
		Qualitys:       Qualitys,
		Subtitles:      Subtitles,
		Audios:         Audios,
	}

	return fiber.StatusOK, &response, nil
}
