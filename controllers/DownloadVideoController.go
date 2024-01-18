package controllers

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func DownloadVideoController(c echo.Context) error {
	type Request struct {
		UUID    string `validate:"required,uuid_rfc4122" param:"UUID"`
		QUALITY string `validate:"required,min=1,max=10" param:"QUALITY"`
	}
	var requestValidation Request
	if status, err := helpers.Validate(c, &requestValidation); err != nil {
		return c.String(status, err.Error())
	}

	reQUALITY := regexp.MustCompile(`^([0-9]{3,4}p|(h264|vp9|av1))$`)
	if !reQUALITY.MatchString(requestValidation.QUALITY) {
		return c.String(http.StatusBadRequest, "bad quality format")
	}

	//translate link id to file id
	var dbLink models.Link
	if dbRes := inits.DB.
		Model(&models.Link{}).
		Preload("File").
		Preload("File.Subtitles").
		Preload("File.Audios").
		Preload("File.Qualitys").
		Where(&models.Link{
			UUID: requestValidation.UUID,
		}).
		First(&dbLink); dbRes.Error != nil {
		return c.String(http.StatusBadRequest, "video doesn't exist")
	}
	files := []string{}
	streamIndex := 0
	// add video
	for _, quality := range dbLink.File.Qualitys {
		if quality.Name == requestValidation.QUALITY {
			files = append(files, "-i", fmt.Sprintf(
				"%s/%s",
				quality.Path,
				quality.OutputFile,
			))
			streamIndex++
		}
	}

	// add audios
	for _, audio := range dbLink.File.Audios {
		files = append(files, "-i", fmt.Sprintf(
			"%s/%s",
			audio.Path,
			audio.OutputFile,
		))
		streamIndex++
	}

	// add subtitles
	for _, subtitle := range dbLink.File.Subtitles {
		files = append(files, "-i", fmt.Sprintf(
			"%s/%s",
			subtitle.Path,
			subtitle.OutputFile,
		))
		streamIndex++
	}

	for i := 0; i < streamIndex; i++ {
		files = append(files, "-map", fmt.Sprintf("%d:0", i))
	}

	tmpFilePath := fmt.Sprintf("%s/%s-tmp-enc.mkv", config.ENV.FolderVideoUploadsPriv, uuid.NewString())
	defer os.Remove(tmpFilePath)

	cmdString := append(files, []string{"-c", "copy", "-f", "matroska", tmpFilePath}...)
	cmd := exec.Command("ffmpeg", cmdString...)

	if err := cmd.Start(); err != nil {
		c.Logger().Error("Failed to run cmd", err)
		return nil
	}

	// wait until file exists
	var tmpFile *os.File
	for {
		if tmpFile == nil {
			f, err := os.Open(tmpFilePath)
			if err != nil {
				time.Sleep(time.Second * 1)
				continue
			}
			tmpFile = f
			break
		}
	}
	// pipe, err := cmd.StdoutPipe()
	// if err != nil {
	// 	c.Logger().Error("Failed to create stdout pipe", err)
	// 	return nil
	// }
	defer tmpFile.Close()

	fileName := regexp.MustCompile(`[^a-zA-Z0-9]+`).ReplaceAllString(dbLink.Name, "-")
	if !strings.HasSuffix(fileName, ".mkv") {
		fileName = fileName + ".mkv"
	}

	c.Response().Header().Add("Content-Type", "video/x-matroska")
	c.Response().Header().Add("Transfer-Encoding", "chunked")
	c.Response().Header().Add("Trailer", "AtEnd")
	c.Response().Header().Add("Cache-Control", "no-cache")
	c.Response().Header().Add("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	c.Response().Status = http.StatusOK

	var wg sync.WaitGroup
	var written int64
	var speedA int64 = 10 * 1024
	var speedB int64 = 10 * 1024 * 1024
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			timeStart := time.Now().UnixMilli()
			n, err := io.CopyN(c.Response().Writer, tmpFile, speedA)
			if err != nil {
				// if err.Error() != "EOF" {
				c.Logger().Error("Failed to write to buffer", err)
				// }
				break
			}
			if n > 0 {
				written = written + n
			}
			c.Response().Flush()
			timeEnd := time.Now().UnixMilli()
			timeDif := timeEnd - timeStart
			// timeout 1 second minus the download time
			time.Sleep(time.Second - (time.Millisecond * time.Duration(timeDif)))
			// increase speed gradualy
			if speedA < speedB {
				speedA = speedA * 2
				if speedA > speedB {
					speedA = speedB
				}
			}
		}
	}()
	wg.Wait()

	cmd.Wait()
	c.Response().Header().Set("Content-Length", fmt.Sprintf("%d", c.Response().Size))
	return nil
}
