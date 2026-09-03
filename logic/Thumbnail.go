package logic

import (
	"ch/kirari04/videocms/storage"
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/labstack/echo/v4"
)

func (s *Service) CreateThumbnail(imageCountAxis int, inputFile string, height int, outputFile string, fileUUID string, videoDuration float64, fps float64) (status int, err error) {
	return s.CreateThumbnailInStore(imageCountAxis, inputFile, height, outputFile, fileUUID, "", videoDuration, fps)
}

func (s *Service) CreateThumbnailInStore(imageCountAxis int, inputFile string, height int, outputFile string, fileUUID string, storeID string, videoDuration float64, fps float64) (status int, err error) {
	return s.CreateThumbnailInStoreContext(context.Background(), imageCountAxis, inputFile, height, outputFile, fileUUID, storeID, videoDuration, fps)
}

func (s *Service) CreateThumbnailInStoreContext(ctx context.Context, imageCountAxis int, inputFile string, height int, outputFile string, fileUUID string, storeID string, videoDuration float64, fps float64) (status int, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	absInputFile, err := filepath.Abs(inputFile)
	if err != nil {
		return http.StatusBadRequest, err
	}
	if s.Deps == nil || s.Deps.Storage == nil || s.Deps.Storage.Workspace() == nil {
		return http.StatusInternalServerError, storage.ErrStoreNotConfigured
	}
	tempOutputFolder, cleanupOutput, err := s.Deps.Storage.Workspace().TempDir(ctx, "generated-thumbnail")
	if err != nil {
		return http.StatusInternalServerError, err
	}
	defer cleanupOutput()

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

	outputPath := filepath.Join(tempOutputFolder, outputFile)
	ffmpegCommand += fmt.Sprintf("%s -y", outputPath)

	ffmpegCommandSimpleImage := fmt.Sprintf(
		`ffmpeg -i %s -ss %.2f -vf scale=-1:%d -vframes 1 %s -y`,
		absInputFile,
		videoDuration/2,
		imageFullHeight,
		outputPath,
	)

	cmd := exec.CommandContext(ctx,
		"bash",
		"-c",
		ffmpegCommand,
	)

	if err := cmd.Run(); err != nil {
		log.Printf("Failed during thumbnail conversion: %s", ffmpegCommand)

		// if tiles fail try simple one instead
		cmd := exec.CommandContext(ctx,
			"bash",
			"-c",
			ffmpegCommandSimpleImage,
		)
		if err := cmd.Run(); err != nil {
			log.Printf("Failed during simple thumbnail conversion: %v : %s", err, ffmpegCommandSimpleImage)
			return http.StatusInternalServerError, echo.ErrInternalServerError
		}

	}

	store, layout, err := s.mediaStorage(storeID)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	key, err := layout.Thumbnail(fileUUID, outputFile)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	output, err := os.Open(outputPath)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	defer output.Close()
	info, err := output.Stat()
	if err != nil {
		return http.StatusInternalServerError, err
	}
	expectedSize := info.Size()
	if _, err := store.Put(ctx, key, output, storage.PutOptions{
		ContentType:  "image/webp",
		CacheControl: "public, max-age=3600",
		ExpectedSize: &expectedSize,
	}); err != nil {
		return http.StatusInternalServerError, err
	}

	return http.StatusOK, nil
}
