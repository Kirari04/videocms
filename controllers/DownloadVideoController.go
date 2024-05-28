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
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func DownloadVideoController(c echo.Context) error {
	type Request struct {
		UUID    string `validate:"required,uuid_rfc4122" param:"UUID"`
		QUALITY string `validate:"required,min=1,max=10" param:"QUALITY"`
		Stream  *bool  `validate:"omitempty,boolean" query:"stream"`
	}
	var requestValidation Request
	if status, err := helpers.Validate(c, &requestValidation); err != nil {
		return c.String(status, err.Error())
	}

	if requestValidation.Stream == nil {
		requestValidation.Stream = new(bool)
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

	if !*requestValidation.Stream {
		// add subtitles
		for _, subtitle := range dbLink.File.Subtitles {
			files = append(files, "-i", fmt.Sprintf(
				"%s/%s",
				subtitle.Path,
				subtitle.OutputFile,
			))
			streamIndex++
		}
	}

	// add audios
	if !*requestValidation.Stream {
		for _, audio := range dbLink.File.Audios {
			files = append(files, "-i", fmt.Sprintf(
				"%s/%s",
				audio.Path,
				audio.OutputFile,
			))
			streamIndex++
		}
	} else {
		if len(dbLink.File.Audios) > 0 {
			files = append(files, "-i", fmt.Sprintf(
				"%s/%s",
				dbLink.File.Audios[0].Path,
				dbLink.File.Audios[0].OutputFile,
			))
			streamIndex++
		}
	}

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

	for i := 0; i < streamIndex; i++ {
		files = append(files, "-map", fmt.Sprintf("%d", i))
	}

	tmpFilePath := fmt.Sprintf("%s/%s-tmp-enc.mp4", config.ENV.FolderVideoUploadsPriv, uuid.NewString())
	defer os.Remove(tmpFilePath)
	var cmdString []string
	if !*requestValidation.Stream {
		cmdString = append(files, []string{"-c", "copy", "-f", "matroska", tmpFilePath}...)
	} else {
		cmdString = append(files, []string{"-c", "copy", "-f", "mp4", tmpFilePath}...)
	}
	cmd := exec.Command("ffmpeg", cmdString...)

	if err := cmd.Start(); err != nil {
		c.Logger().Error("Failed to run cmd", err)
		return nil
	}

	if *requestValidation.Stream {
		if err := cmd.Wait(); err != nil {
			c.Logger().Error("Failed to run cmd on wait", err)
			return nil
		}
	} else {
		defer cmd.Wait()
	}

	// wait until file exists
	var tmpFile *os.File
	var fileName string
	if *requestValidation.Stream {
		f, err := os.Open(tmpFilePath)
		if err != nil {
			c.Logger().Error("Failed to open tmp file", err)
			return nil
		}
		tmpFile = f
		fileName = fmt.Sprintf(
			"%s[%s].mp4",
			regexp.MustCompile(`[^a-zA-Z0-9]+`).ReplaceAllString(dbLink.Name, "-"),
			requestValidation.QUALITY,
		)
	} else {
		var try = 0
		for {
			if try > 10 {
				c.Logger().Error("Failed to receive output file from ffmpeg")
				return nil
			}
			if tmpFile == nil {
				f, err := os.Open(tmpFilePath)
				if err != nil {
					try++
					time.Sleep(time.Second * 1)
					continue
				}
				tmpFile = f
				break
			}
		}
		fileName = fmt.Sprintf(
			"%s[%s].mkv",
			regexp.MustCompile(`[^a-zA-Z0-9]+`).ReplaceAllString(dbLink.Name, "-"),
			requestValidation.QUALITY,
		)
	}
	defer tmpFile.Close()

	if *requestValidation.Stream {
		c.Response().Header().Add("Accept-Ranges", "bytes")
		http.ServeContent(c.Response(), c.Request(), fileName, time.Now(), tmpFile)
	} else {
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
					if err.Error() != "EOF" {
						c.Logger().Error("Failed to write to buffer", err)
					}
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
	}
	return nil
}
