package controllers

import (
	"bufio"
	"bytes"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"regexp"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

func DownloadVideoController(c *fiber.Ctx) error {
	type Request struct {
		UUID    string `validate:"required,uuid_rfc4122"`
		QUALITY string `validate:"required,min=1,max=10"`
	}
	var requestValidation Request
	if err := c.ParamsParser(&requestValidation); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(requestValidation); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	reQUALITY := regexp.MustCompile(`^([0-9]{3,4}p|(h264|vp9|av1))$`)
	if !reQUALITY.MatchString(requestValidation.QUALITY) {
		return c.Status(fiber.StatusBadRequest).SendString("bad quality format")
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
		return c.Status(fiber.StatusBadRequest).SendString("video doesn't exist")
	}
	files := []string{}

	// add video
	for _, quality := range dbLink.File.Qualitys {
		if quality.Name == requestValidation.QUALITY {
			files = append(files, "-i", fmt.Sprintf(
				"%s/%s",
				quality.Path,
				quality.OutputFile,
			))
		}
	}

	// add audios
	for _, audio := range dbLink.File.Audios {
		files = append(files, "-i", fmt.Sprintf(
			"%s/%s",
			audio.Path,
			audio.OutputFile,
		))
	}

	// add subtitles
	for _, subtitle := range dbLink.File.Subtitles {
		files = append(files, "-i", fmt.Sprintf(
			"%s/%s",
			subtitle.Path,
			subtitle.OutputFile,
		))
	}

	cmdString := append(files, []string{"-map", "0", "-c", "copy", "-f", "matroska", "pipe:1"}...)

	cmd := exec.Command("ffmpeg", cmdString...)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		log.Println("Failed to create stdout pipe")
		return nil
	}

	if err := cmd.Start(); err != nil {
		log.Println("Failed to run cmd", err)
		return nil
	}

	c.Response().StreamBody = true
	c.Response().ImmediateHeaderFlush = true

	c.Response().Header.Set(fiber.HeaderContentType, "video/x-matroska")
	c.Response().Header.Set(fiber.HeaderTransferEncoding, "chunked")
	c.Response().Header.Set(fiber.HeaderTrailer, "AtEnd")
	c.Response().Header.Set(fiber.HeaderCacheControl, "no-cache")
	c.Response().Header.Set(fiber.HeaderContentDisposition, `attachment; filename="video.mkv"`)
	c.Status(http.StatusOK)

	var wg sync.WaitGroup
	var written int64
	var speedA int64 = 10 * 1024
	var speedB int64 = 10 * 1024 * 1024
	writer := make(chan []byte, 10*1024*1024)
	c.Response().SetBodyStreamWriter(func(w *bufio.Writer) {
		for {
			b, ok := <-writer
			if !ok {
				log.Println("stopped channel")
				break
			}
			w.Write(b)
			w.Flush()
		}
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer pipe.Close()
		for {
			timeStart := time.Now().UnixMilli()
			var b bytes.Buffer
			n, err := io.CopyN(&b, pipe, speedA)
			if err != nil {
				if err.Error() != "EOF" {
					log.Println("Failed to write to buffer", err)
				}
				break
			}
			if n > 0 {
				writer <- b.Bytes()
				written = written + n
			}
			timeEnd := time.Now().UnixMilli()
			timeDif := timeEnd - timeStart
			c.Response().Header.Set(fiber.HeaderContentLength, fmt.Sprintf("%d", written))
			time.Sleep(time.Second - (time.Millisecond * time.Duration(timeDif)))
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
	close(writer)
	log.Println("finished")
	return nil
}
