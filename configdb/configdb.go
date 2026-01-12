package configdb

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"log"
	"strconv"

	"github.com/go-playground/validator/v10"
	"github.com/xyproto/randomstring"
)

func Setup() {
	var setting models.Setting
	if res := inits.DB.FirstOrCreate(&setting); res.Error != nil {
		log.Fatalln("Failed to load settings", res.Error)
	}

	config.ENV.AppName = getEnvDb(&setting.AppName, "VideoCMS")
	config.ENV.BaseUrl = getEnvDb(&setting.BaseUrl, "http://127.0.0.1:3000")

	config.ENV.Project = getEnvDb(&setting.Project, "https://github.com/notfound")
	config.ENV.ProjectDocumentation = getEnvDb(&setting.ProjectDocumentation, "https://github.com/notfound")
	config.ENV.ProjectDownload = getEnvDb(&setting.ProjectDownload, "https://github.com/notfound")
	config.ENV.ProjectExampleVideo = getEnvDb(&setting.ProjectExampleVideo, "notfound")

	config.ENV.JwtSecretKey = getEnvDb(&setting.JwtSecretKey, randomstring.CookieFriendlyString(64))
	config.ENV.JwtUploadSecretKey = getEnvDb(&setting.JwtUploadSecretKey, randomstring.CookieFriendlyString(64))

	config.ENV.ReloadHtml = getEnvDb_bool(&setting.ReloadHtml, boolPtr(false))
	config.ENV.EncodingEnabled = getEnvDb_bool(&setting.EncodingEnabled, boolPtr(true))
	config.ENV.UploadEnabled = getEnvDb_bool(&setting.UploadEnabled, boolPtr(true))
	config.ENV.RatelimitEnabled = getEnvDb_bool(&setting.RatelimitEnabled, boolPtr(false))
	config.ENV.CloudflareEnabled = getEnvDb_bool(&setting.CloudflareEnabled, boolPtr(false))

	config.ENV.MaxItemsMultiDelete = getEnvDb_int64(&setting.MaxItemsMultiDelete, 1000)
	config.ENV.MaxRunningEncodes = getEnvDb_int64(&setting.MaxRunningEncodes, 1)
	config.ENV.MaxFramerate = getEnvDb_int64(&setting.MaxFramerate, 60)

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
	config.ENV.Hls240pVideoBitrate = getEnvDb(&setting.Hls240pVideoBitrate, "600k")
	config.ENV.EncodeHls360p = getEnvDb_bool(&setting.EncodeHls360p, boolPtr(true))
	config.ENV.Hls360pVideoBitrate = getEnvDb(&setting.Hls360pVideoBitrate, "1200k")
	config.ENV.EncodeHls480p = getEnvDb_bool(&setting.EncodeHls480p, boolPtr(true))
	config.ENV.Hls480pVideoBitrate = getEnvDb(&setting.Hls480pVideoBitrate, "2500k")
	config.ENV.EncodeHls720p = getEnvDb_bool(&setting.EncodeHls720p, boolPtr(true))
	config.ENV.Hls720pVideoBitrate = getEnvDb(&setting.Hls720pVideoBitrate, "4500k")
	config.ENV.EncodeHls1080p = getEnvDb_bool(&setting.EncodeHls1080p, boolPtr(true))
	config.ENV.Hls1080pVideoBitrate = getEnvDb(&setting.Hls1080pVideoBitrate, "8000k")
	config.ENV.EncodeHls1440p = getEnvDb_bool(&setting.EncodeHls1440p, boolPtr(false))
	config.ENV.Hls1440pVideoBitrate = getEnvDb(&setting.Hls1440pVideoBitrate, "15000k")
	config.ENV.EncodeHls2160p = getEnvDb_bool(&setting.EncodeHls2160p, boolPtr(false))
	config.ENV.Hls2160pVideoBitrate = getEnvDb(&setting.Hls2160pVideoBitrate, "25000k")

	config.ENV.PluginPgsServer = getEnvDb(&setting.PluginPgsServer, "http://127.0.0.1:5000")
	config.ENV.EnablePluginPgsServer = getEnvDb_bool(&setting.EnablePluginPgsServer, boolPtr(false))

	config.ENV.DownloadEnabled = getEnvDb_bool(&setting.DownloadEnabled, boolPtr(true))
	config.ENV.ContinueWatchingPopupEnabled = getEnvDb_bool(&setting.ContinueWatchingPopupEnabled, boolPtr(true))

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

			VideoBitrate:   config.ENV.Hls240pVideoBitrate,
			AudioBitrate:   "64k",
			Profile:        "baseline",
			Level:          "1.3",
			CodecStringAVC: "avc1.42e00d",

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

			VideoBitrate:   config.ENV.Hls360pVideoBitrate,
			AudioBitrate:   "96k",
			Profile:        "main",
			Level:          "3.0",
			CodecStringAVC: "avc1.4d401e",

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

			VideoBitrate:   config.ENV.Hls480pVideoBitrate,
			AudioBitrate:   "128k",
			Profile:        "main",
			Level:          "3.1",
			CodecStringAVC: "avc1.4d401f",

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

			VideoBitrate:   config.ENV.Hls720pVideoBitrate,
			AudioBitrate:   "192k",
			Profile:        "high",
			Level:          "4.0",
			CodecStringAVC: "avc1.64001f",

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

			VideoBitrate:   config.ENV.Hls1080pVideoBitrate,
			AudioBitrate:   "256k",
			Profile:        "high",
			Level:          "4.2",
			CodecStringAVC: "avc1.64002a",

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

			VideoBitrate:   config.ENV.Hls1440pVideoBitrate,
			AudioBitrate:   "256k",
			Profile:        "high",
			Level:          "5.1",
			CodecStringAVC: "avc1.640033",

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

			VideoBitrate:   config.ENV.Hls2160pVideoBitrate,
			AudioBitrate:   "320k",
			Profile:        "high",
			Level:          "5.2",
			CodecStringAVC: "avc1.640034",

			Type:       "hls",
			Muted:      true,
			OutputFile: "out.m3u8",
			Enabled:    *config.ENV.EncodeHls2160p,
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
