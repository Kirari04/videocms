package logic

import (
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/gofiber/fiber/v2"
)

func CreateThumbnail(imageCountAxis int, inputFile string, height int, outputFile string, outputFolder string, videoDuration float64, fps float64) (status int, err error) {
	// read file & folder
	absOutputFolder, err := filepath.Abs(outputFolder)
	if err != nil {
		return fiber.StatusBadRequest, err
	}
	os.MkdirAll(absOutputFolder, 0777)

	absInputFile, err := filepath.Abs(inputFile)
	if err != nil {
		return fiber.StatusBadRequest, err
	}

	// build ffmpeg command
	imageCount := imageCountAxis * imageCountAxis
	imageSingeHeight := int(math.RoundToEven(float64(height/imageCountAxis)/2) * 2)
	imageFullHeight := imageSingeHeight * imageCountAxis

	ffmpegCommand := fmt.Sprintf("ffmpeg -i %s ", absInputFile)
	filterComplex := fmt.Sprintf(
		`-filter_complex "[0:v]scale=-1:%d[bg];[0:v]scale=-1:%d[img];`,
		imageFullHeight,
		imageSingeHeight,
	)
	// filter compelx filter splitting
	filterComplexSplit := fmt.Sprintf("[img]split=%d", imageCount)
	for i := 0; i < imageCount; i++ {
		filterComplexSplit += fmt.Sprintf("[v%d]", i)
	}
	filterComplexSplit += ";"
	filterComplex += filterComplexSplit

	// filter complex overlay
	filterComplexOverlay := ""
	filterComplexOverlayPositionX := 0
	filterComplexOverlayPositionY := 0
	filterComplexOverlayPositionM := imageCountAxis - 1
	for i := 0; i < imageCount; i++ {
		var filterInput string
		if i == 0 {
			filterInput = "[bg]"
		} else {
			filterInput = fmt.Sprintf("[f%d]", i)
		}

		videoStartTime := (videoDuration - 0.1) / float64(imageCount) * float64(i+1)
		filterComplexOverlay += fmt.Sprintf(
			"%s[v%d]overlay=w*%d:h*%d,trim=start=%.2f:end=%.2f[f%d];",
			filterInput,
			i,
			filterComplexOverlayPositionX,
			filterComplexOverlayPositionY,
			videoStartTime,
			videoStartTime+0.1,
			i+1,
		)
		filterComplexOverlayPositionX++
		// this will check if the next filterComplexOverlayPositionX is over the limit and set the new counter
		if filterComplexOverlayPositionX > filterComplexOverlayPositionM {
			filterComplexOverlayPositionX = 0
			filterComplexOverlayPositionY++
		}
	}
	filterComplexOverlay += fmt.Sprintf(
		`[f%d]setpts=PTS-STARTPTS,scale=-1:%d[fin]" `,
		imageCount,
		imageFullHeight,
	)

	filterComplex += filterComplexOverlay
	ffmpegCommand += filterComplex

	ffmpegCommand += fmt.Sprintf("-map [fin] -vframes 1 %s/%s -y", absOutputFolder, outputFile)

	ffmpegCommandSimpleImage := fmt.Sprintf(
		`ffmpeg -i %s -ss %.2f -vf scale=-1:%d -vframes 1 %s/%s -y`,
		absInputFile,
		videoDuration/2,
		imageFullHeight,
		absOutputFolder,
		outputFile,
	)

	cmd := exec.Command(
		"bash",
		"-c",
		ffmpegCommand,
	)

	if err := cmd.Run(); err != nil {
		// try simple one instead
		cmd := exec.Command(
			"bash",
			"-c",
			ffmpegCommandSimpleImage,
		)
		if err := cmd.Run(); err != nil {
			log.Printf("Failed during thumbnail conversion: %s", ffmpegCommand)
			log.Printf("Failed during simple thumbnail conversion: %s", ffmpegCommandSimpleImage)
			return fiber.StatusInternalServerError, err
		}

	}

	return fiber.StatusOK, nil
}
