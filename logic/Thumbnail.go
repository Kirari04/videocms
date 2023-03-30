package logic

import (
	"errors"
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

	ffmpegCommand := fmt.Sprintf("ffmpeg -i %s -vframes 1 ", absInputFile)
	filterComplex := `-filter_complex "`

	// filter complex overlay
	filterComplexStackPositionX := 0
	filterComplexStackPositionY := 0
	filterComplexStackPositionM := imageCountAxis - 1
	for i := 0; i < imageCount; i++ {
		videoStartTimeFrame := math.Floor((videoDuration / float64(imageCount+1)) * float64(i+1) * math.Floor(fps))
		filterComplex += fmt.Sprintf(
			"[0:v]select='eq(n,%.0f)',scale=iw/%d:-1[X%dY%d];",
			videoStartTimeFrame,
			imageCountAxis,
			filterComplexStackPositionX,
			filterComplexStackPositionY,
		)
		filterComplexStackPositionX++
		// this will check if the next filterComplexStackPositionX is over the limit and set the new counter
		if filterComplexStackPositionX > filterComplexStackPositionM {
			filterComplexStackPositionX = 0
			filterComplexStackPositionY++
		}
	}
	// add left to right
	for i := 0; i < imageCountAxis; i++ {
		inputs := ""
		for ii := 0; ii < imageCountAxis; ii++ {
			inputs += fmt.Sprintf("[X%dY%d]", ii, i)
		}
		filterComplex += fmt.Sprintf("%shstack=inputs=%d[R%d];", inputs, imageCountAxis, i)
	}

	// add top to bottom
	inputs := ""
	for i := 0; i < imageCountAxis; i++ {
		inputs += fmt.Sprintf("[R%d]", i)
	}
	filterComplex += fmt.Sprintf(`%svstack=inputs=%d" `, inputs, imageCountAxis)

	ffmpegCommand += filterComplex

	ffmpegCommand += fmt.Sprintf("%s/%s -y", absOutputFolder, outputFile)

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
		log.Printf("Failed during thumbnail conversion: %s", ffmpegCommand)

		// if tiles fail try simple one instead
		cmd := exec.Command(
			"bash",
			"-c",
			ffmpegCommandSimpleImage,
		)
		if err := cmd.Run(); err != nil {
			log.Printf("Failed during simple thumbnail conversion: %v : %s", err, ffmpegCommandSimpleImage)
			return fiber.StatusInternalServerError, errors.New("")
		}

	}

	return fiber.StatusOK, nil
}
