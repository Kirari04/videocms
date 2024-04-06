package configdb

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"log"
	"strconv"

	"github.com/go-playground/validator/v10"
)

func Setup() {
	var setting models.Setting
	if res := inits.DB.FirstOrCreate(&setting); res.Error != nil {
		log.Fatalln("Failed to load settings", res.Error)
	}

	config.ENV.AppName = getEnvDb(&setting.AppName, "VideoCMS")

	config.ENV.Project = getEnvDb(&setting.Project, "https://github.com/notfound")
	config.ENV.ProjectDocumentation = getEnvDb(&setting.ProjectDocumentation, "https://github.com/notfound")
	config.ENV.ProjectDownload = getEnvDb(&setting.ProjectDownload, "https://github.com/notfound")
	config.ENV.ProjectExampleVideo = getEnvDb(&setting.ProjectExampleVideo, "notfound")

	config.ENV.JwtSecretKey = getEnvDb(&setting.JwtSecretKey, "secretkey")
	config.ENV.JwtUploadSecretKey = getEnvDb(&setting.JwtUploadSecretKey, "secretkeyupload")

	config.ENV.CookieDomain = getEnvDb(&setting.CookieDomain, "secretkey")

	config.ENV.ReloadHtml = getEnvDb_bool(&setting.ReloadHtml, boolPtr(false))
	config.ENV.EncodingEnabled = getEnvDb_bool(&setting.EncodingEnabled, boolPtr(true))
	config.ENV.UploadEnabled = getEnvDb_bool(&setting.UploadEnabled, boolPtr(true))
	config.ENV.RatelimitEnabled = getEnvDb_bool(&setting.RatelimitEnabled, boolPtr(false))
	config.ENV.CloudflareEnabled = getEnvDb_bool(&setting.CloudflareEnabled, boolPtr(false))

	config.ENV.MaxItemsMultiDelete = getEnvDb_int64(&setting.MaxItemsMultiDelete, 1000)
	config.ENV.MaxRunningEncodes = getEnvDb_int64(&setting.MaxRunningEncodes, 1)

	config.ENV.MaxUploadFilesize = getEnvDb_int64(&setting.MaxUploadFilesize, 5*1024*1024*1024) // 5gb
	config.ENV.MaxUploadChuncksize = getEnvDb_int64(&setting.MaxUploadChuncksize, 20*1024*1024) // 20mb
	config.ENV.MaxUploadSessions = getEnvDb_int64(&setting.MaxUploadSessions, 10)
	config.ENV.MaxPostSize = getEnvDb_int64(&setting.MaxPostSize, 100*1024*1024) // 100mb

	config.ENV.CorsAllowHeaders = getEnvDb(&setting.CorsAllowHeaders, "*")
	config.ENV.CorsAllowOrigins = getEnvDb(&setting.CorsAllowOrigins, "*")
	config.ENV.CorsAllowCredentials = getEnvDb_bool(&setting.CorsAllowCredentials, boolPtr(true))

	config.ENV.CaptchaEnabled = getEnvDb_bool(&setting.CaptchaEnabled, boolPtr(false))
	config.ENV.CaptchaType = getEnvDb(&setting.CaptchaType, "")
	config.ENV.Captcha_Recaptcha_PrivateKey = getEnvDb(&setting.Captcha_Recaptcha_PrivateKey, "")
	config.ENV.Captcha_Recaptcha_PublicKey = getEnvDb(&setting.Captcha_Recaptcha_PublicKey, "")
	config.ENV.Captcha_Hcaptcha_PrivateKey = getEnvDb(&setting.Captcha_Hcaptcha_PrivateKey, "")
	config.ENV.Captcha_Hcaptcha_PublicKey = getEnvDb(&setting.Captcha_Hcaptcha_PublicKey, "")

	config.ENV.EncodeHls240p = getEnvDb_bool(&setting.EncodeHls240p, boolPtr(true))
	config.ENV.EncodeHls360p = getEnvDb_bool(&setting.EncodeHls360p, boolPtr(true))
	config.ENV.EncodeHls480p = getEnvDb_bool(&setting.EncodeHls480p, boolPtr(true))
	config.ENV.EncodeHls720p = getEnvDb_bool(&setting.EncodeHls720p, boolPtr(true))
	config.ENV.EncodeHls1080p = getEnvDb_bool(&setting.EncodeHls1080p, boolPtr(true))
	config.ENV.EncodeHls1440p = getEnvDb_bool(&setting.EncodeHls1440p, boolPtr(false))
	config.ENV.EncodeHls2160p = getEnvDb_bool(&setting.EncodeHls2160p, boolPtr(false))
	config.ENV.EncodeAv1 = getEnvDb_bool(&setting.EncodeAv1, boolPtr(false))
	config.ENV.EncodeVp9 = getEnvDb_bool(&setting.EncodeVp9, boolPtr(false))
	config.ENV.EncodeH264 = getEnvDb_bool(&setting.EncodeH264, boolPtr(false))

	config.ENV.FFmpegAv1AudioCodec = getEnvDb(&setting.FFmpegAv1AudioCodec, "aac")
	config.ENV.FFmpegVp9AudioCodec = getEnvDb(&setting.FFmpegVp9AudioCodec, "libopus")
	config.ENV.FFmpegH264AudioCodec = getEnvDb(&setting.FFmpegH264AudioCodec, "aac")

	config.ENV.FFmpegAv1Crf = getEnvDb_int(&setting.FFmpegAv1Crf, 30)
	config.ENV.FFmpegVp9Crf = getEnvDb_int(&setting.FFmpegVp9Crf, 30)
	config.ENV.FFmpegH264Crf = getEnvDb_int(&setting.FFmpegH264Crf, 30)

	config.ENV.FFmpegAv1Height = getEnvDb_int64(&setting.FFmpegAv1Height, 480)
	config.ENV.FFmpegAv1Width = getEnvDb_int64(&setting.FFmpegAv1Width, 854)
	config.ENV.FFmpegVp9Height = getEnvDb_int64(&setting.FFmpegVp9Height, 480)
	config.ENV.FFmpegVp9Width = getEnvDb_int64(&setting.FFmpegVp9Width, 854)
	config.ENV.FFmpegH264Height = getEnvDb_int64(&setting.FFmpegH264Height, 480)
	config.ENV.FFmpegH264Width = getEnvDb_int64(&setting.FFmpegH264Width, 854)

	config.ENV.PluginPgsServer = getEnvDb(&setting.PluginPgsServer, "http://127.0.0.1:5000")

	// validate config before saving
	validate := validator.New(validator.WithRequiredStructEnabled())
	err := validate.Struct(&setting)
	if err != nil {
		log.Fatalln("Invalid configuration", err)
	}
	if res := inits.DB.Save(&setting); res.Error != nil {
		log.Fatalln("failed to save db configuration", res.Error)
	}

	models.AvailableQualitys = []models.AvailableQuality{
		{
			Name:       "240p",
			FolderName: "240p",
			Height:     240,
			Width:      426,
			Crf:        30,
			Type:       "hls",
			Muted:      true,
			OutputFile: "out.m3u8",
			Enabled:    *config.ENV.EncodeHls240p,
		},
		{
			Name:       "360p",
			FolderName: "360p",
			Height:     360,
			Width:      640,
			Crf:        26,
			Type:       "hls",
			Muted:      true,
			OutputFile: "out.m3u8",
			Enabled:    *config.ENV.EncodeHls360p,
		},
		{
			Name:       "480p",
			FolderName: "480p",
			Height:     480,
			Width:      854,
			Crf:        26,
			Type:       "hls",
			Muted:      true,
			OutputFile: "out.m3u8",
			Enabled:    *config.ENV.EncodeHls480p,
		},
		{
			Name:       "720p",
			FolderName: "720p",
			Height:     720,
			Width:      1280,
			Crf:        26,
			Type:       "hls",
			Muted:      true,
			OutputFile: "out.m3u8",
			Enabled:    *config.ENV.EncodeHls720p,
		},
		{
			Name:       "1080p",
			FolderName: "1080p",
			Height:     1080,
			Width:      1920,
			Crf:        24,
			Type:       "hls",
			Muted:      true,
			OutputFile: "out.m3u8",
			Enabled:    *config.ENV.EncodeHls1080p,
		},
		{
			Name:       "1440p",
			FolderName: "1440p",
			Height:     1440,
			Width:      2560,
			Crf:        24,
			Type:       "hls",
			Muted:      true,
			OutputFile: "out.m3u8",
			Enabled:    *config.ENV.EncodeHls1440p,
		},
		{
			Name:       "2160p",
			FolderName: "2160p",
			Height:     2160,
			Width:      3840,
			Crf:        24,
			Type:       "hls",
			Muted:      true,
			OutputFile: "out.m3u8",
			Enabled:    *config.ENV.EncodeHls2160p,
		},
		// downloads
		{
			Name:       "av1",
			FolderName: "av1",
			Height:     config.ENV.FFmpegAv1Height,
			Width:      config.ENV.FFmpegAv1Width,
			Crf:        config.ENV.FFmpegAv1Crf,
			Type:       "av1",
			Muted:      false,
			AudioCodec: config.ENV.FFmpegAv1AudioCodec,
			OutputFile: "out.mp4",
			Enabled:    *config.ENV.EncodeAv1,
		},
		{
			Name:       "vp9",
			FolderName: "vp9",
			Height:     config.ENV.FFmpegVp9Height,
			Width:      config.ENV.FFmpegVp9Width,
			Crf:        config.ENV.FFmpegVp9Crf,
			Type:       "vp9",
			Muted:      false,
			AudioCodec: config.ENV.FFmpegVp9AudioCodec,
			OutputFile: "out.webm",
			Enabled:    *config.ENV.EncodeVp9,
		},
		{
			Name:       "h264",
			FolderName: "h264",
			Height:     config.ENV.FFmpegH264Height,
			Width:      config.ENV.FFmpegH264Width,
			Crf:        config.ENV.FFmpegH264Crf,
			Type:       "h264",
			Muted:      false,
			AudioCodec: config.ENV.FFmpegH264AudioCodec,
			OutputFile: "out.mp4",
			Enabled:    *config.ENV.EncodeH264,
		},
	}
}

func getEnvDb(value *string, defaultValue string) string {
	if value != nil && *value != "" {
		return *value
	}
	*value = defaultValue
	return defaultValue
}

func getEnvDb_bool(value *string, defaultValue *bool) *bool {
	if value != nil && *value != "" {
		switch *value {
		case "true":
			return boolPtr(true)
		case "1":
			return boolPtr(true)
		case "false":
			return boolPtr(false)
		case "0":
			return boolPtr(false)
		default:
			log.Panicf("Failed to get bool from value: %v", value)
		}
	}
	if defaultValue != nil && *defaultValue == true {
		*value = "true"
	} else {
		*value = "false"
	}
	return defaultValue
}

func getEnvDb_int64(value *string, defaultValue int64) int64 {
	if value != nil && *value != "" {
		res, err := strconv.ParseInt(*value, 10, 64)
		if err != nil {
			log.Panicf("Failed to parse int from value %v", value)
		}
		return res
	}
	*value = fmt.Sprint(defaultValue)
	return defaultValue
}
func getEnvDb_int(value *string, defaultValue int) int {
	if value != nil && *value != "" {
		res, err := strconv.Atoi(*value)
		if err != nil {
			log.Panicf("Failed to parse int from value %v", value)
		}
		return res
	}
	*value = fmt.Sprint(defaultValue)
	return defaultValue
}

func boolPtr(boolean bool) *bool {
	return &boolean
}
