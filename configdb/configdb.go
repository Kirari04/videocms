package configdb

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/models"
	"fmt"
	"log"
	"strconv"

	"github.com/go-playground/validator/v10"
	"github.com/xyproto/randomstring"
	"gorm.io/gorm"
)

func LoadSnapshot(db *gorm.DB, base config.Config) (app.Snapshot, error) {
	env := base.Clone()
	var setting models.Setting
	if res := db.FirstOrCreate(&setting); res.Error != nil {
		return app.Snapshot{}, fmt.Errorf("failed to load settings: %w", res.Error)
	}

	env.AppName = getEnvDb(&setting.AppName, "VideoCMS")
	env.BaseUrl = getEnvDb(&setting.BaseUrl, "http://127.0.0.1:3000")

	env.ProjectExampleVideo = getEnvDb(&setting.ProjectExampleVideo, "notfound")

	env.JwtSecretKey = getEnvDb(&setting.JwtSecretKey, randomstring.CookieFriendlyString(64))
	env.JwtMediaSecretKey = getEnvDb(&setting.JwtMediaSecretKey, randomstring.CookieFriendlyString(64))

	env.ReloadHtml = getEnvDb_bool(&setting.ReloadHtml, boolPtr(false))
	env.EncodingEnabled = getEnvDb_bool(&setting.EncodingEnabled, boolPtr(true))
	env.UploadEnabled = getEnvDb_bool(&setting.UploadEnabled, boolPtr(true))
	env.RatelimitEnabled = getEnvDb_bool(&setting.RatelimitEnabled, boolPtr(false))

	env.RatelimitRateGlobal = getEnvDb_float64(&setting.RatelimitRateGlobal, 100)
	env.RatelimitBurstGlobal = getEnvDb_int(&setting.RatelimitBurstGlobal, 200)
	env.RatelimitRateAuth = getEnvDb_float64(&setting.RatelimitRateAuth, 5)
	env.RatelimitBurstAuth = getEnvDb_int(&setting.RatelimitBurstAuth, 10)
	env.RatelimitRateApi = getEnvDb_float64(&setting.RatelimitRateApi, 50)
	env.RatelimitBurstApi = getEnvDb_int(&setting.RatelimitBurstApi, 100)
	env.RatelimitRateUpload = getEnvDb_float64(&setting.RatelimitRateUpload, 20)
	env.RatelimitBurstUpload = getEnvDb_int(&setting.RatelimitBurstUpload, 50)
	env.RatelimitRateWeb = getEnvDb_float64(&setting.RatelimitRateWeb, 50)
	env.RatelimitBurstWeb = getEnvDb_int(&setting.RatelimitBurstWeb, 100)

	env.CloudflareEnabled = getEnvDb_bool(&setting.CloudflareEnabled, boolPtr(false))
	env.BunnyCDNEnabled = getEnvDb_bool(&setting.BunnyCDNEnabled, boolPtr(false))
	env.FastlyEnabled = getEnvDb_bool(&setting.FastlyEnabled, boolPtr(false))
	env.KeyCDNEnabled = getEnvDb_bool(&setting.KeyCDNEnabled, boolPtr(false))
	env.TrustedProxies = getEnvDb(&setting.TrustedProxies, "")
	env.TrustLocalTraffic = getEnvDb_bool(&setting.TrustLocalTraffic, boolPtr(false))

	env.MaxItemsMultiDelete = getEnvDb_int64(&setting.MaxItemsMultiDelete, 1000)
	env.MaxRunningEncodes = getEnvDb_int64(&setting.MaxRunningEncodes, 1)
	env.MaxFramerate = getEnvDb_int64(&setting.MaxFramerate, 60)

	if setting.MaxUploadChunkSize == "" && setting.LegacyMaxUploadChunkSize != "" {
		setting.MaxUploadChunkSize = setting.LegacyMaxUploadChunkSize
	}
	env.MaxUploadFilesize = getEnvDb_int64(&setting.MaxUploadFilesize, 5*1024*1024*1024) // 5gb
	env.MaxUploadChunkSize = getEnvDb_int64(&setting.MaxUploadChunkSize, 20*1024*1024)   // 20mb
	env.MaxUploadSessions = getEnvDb_int64(&setting.MaxUploadSessions, 10)
	env.MaxPostSize = getEnvDb_int64(&setting.MaxPostSize, 100*1024*1024) // 100mb

	env.CorsAllowHeaders = getEnvDb(&setting.CorsAllowHeaders, "*")
	env.CorsAllowOrigins = getEnvDb(&setting.CorsAllowOrigins, "*")
	env.CorsAllowCredentials = getEnvDb_bool(&setting.CorsAllowCredentials, boolPtr(true))

	env.CaptchaEnabled = getEnvDb_bool(&setting.CaptchaEnabled, boolPtr(false))
	env.CaptchaLoginEnabled = getEnvDb_bool(&setting.CaptchaLoginEnabled, boolPtr(false))
	env.CaptchaPlayerEnabled = getEnvDb_bool(&setting.CaptchaPlayerEnabled, boolPtr(false))
	env.CaptchaType = getEnvDb(&setting.CaptchaType, "")
	env.Captcha_Recaptcha_PrivateKey = getEnvDb(&setting.Captcha_Recaptcha_PrivateKey, "")
	env.Captcha_Recaptcha_PublicKey = getEnvDb(&setting.Captcha_Recaptcha_PublicKey, "")
	env.Captcha_Hcaptcha_PrivateKey = getEnvDb(&setting.Captcha_Hcaptcha_PrivateKey, "")
	env.Captcha_Hcaptcha_PublicKey = getEnvDb(&setting.Captcha_Hcaptcha_PublicKey, "")
	env.Captcha_Turnstile_PrivateKey = getEnvDb(&setting.Captcha_Turnstile_PrivateKey, "")
	env.Captcha_Turnstile_PublicKey = getEnvDb(&setting.Captcha_Turnstile_PublicKey, "")

	env.EncodeHls240p = getEnvDb_bool(&setting.EncodeHls240p, boolPtr(true))
	env.Hls240pVideoBitrate = getEnvDb(&setting.Hls240pVideoBitrate, "600k")
	env.Hls240pCrf = getEnvDb_int(&setting.Hls240pCrf, 23)
	env.EncodeHls360p = getEnvDb_bool(&setting.EncodeHls360p, boolPtr(true))
	env.Hls360pVideoBitrate = getEnvDb(&setting.Hls360pVideoBitrate, "1200k")
	env.Hls360pCrf = getEnvDb_int(&setting.Hls360pCrf, 23)
	env.EncodeHls480p = getEnvDb_bool(&setting.EncodeHls480p, boolPtr(true))
	env.Hls480pVideoBitrate = getEnvDb(&setting.Hls480pVideoBitrate, "2500k")
	env.Hls480pCrf = getEnvDb_int(&setting.Hls480pCrf, 23)
	env.EncodeHls720p = getEnvDb_bool(&setting.EncodeHls720p, boolPtr(true))
	env.Hls720pVideoBitrate = getEnvDb(&setting.Hls720pVideoBitrate, "4500k")
	env.Hls720pCrf = getEnvDb_int(&setting.Hls720pCrf, 23)
	env.EncodeHls1080p = getEnvDb_bool(&setting.EncodeHls1080p, boolPtr(true))
	env.Hls1080pVideoBitrate = getEnvDb(&setting.Hls1080pVideoBitrate, "8000k")
	env.Hls1080pCrf = getEnvDb_int(&setting.Hls1080pCrf, 23)
	env.EncodeHls1440p = getEnvDb_bool(&setting.EncodeHls1440p, boolPtr(false))
	env.Hls1440pVideoBitrate = getEnvDb(&setting.Hls1440pVideoBitrate, "15000k")
	env.Hls1440pCrf = getEnvDb_int(&setting.Hls1440pCrf, 23)
	env.EncodeHls2160p = getEnvDb_bool(&setting.EncodeHls2160p, boolPtr(false))
	env.Hls2160pVideoBitrate = getEnvDb(&setting.Hls2160pVideoBitrate, "25000k")
	env.Hls2160pCrf = getEnvDb_int(&setting.Hls2160pCrf, 23)

	env.PluginPgsServer = getEnvDb(&setting.PluginPgsServer, "http://127.0.0.1:5000")
	env.EnablePluginPgsServer = getEnvDb_bool(&setting.EnablePluginPgsServer, boolPtr(false))

	env.DownloadEnabled = getEnvDb_bool(&setting.DownloadEnabled, boolPtr(true))
	env.RemoteDownloadEnabled = getEnvDb_bool(&setting.RemoteDownloadEnabled, boolPtr(true))
	env.ContinueWatchingPopupEnabled = getEnvDb_bool(&setting.ContinueWatchingPopupEnabled, boolPtr(true))
	env.PlayerV2Enabled = getEnvDb_bool(&setting.PlayerV2Enabled, boolPtr(true))

	env.MaxParallelDownloads = getEnvDb_int64(&setting.MaxParallelDownloads, 1)
	env.RemoteDownloadTimeout = getEnvDb_int64(&setting.RemoteDownloadTimeout, 3600) // 1 hour

	// validate config before saving
	validate := validator.New(validator.WithRequiredStructEnabled())
	err := validate.Struct(&setting)
	if err != nil {
		return app.Snapshot{}, fmt.Errorf("invalid configuration: %w", err)
	}
	if res := db.Save(&setting); res.Error != nil {
		return app.Snapshot{}, fmt.Errorf("failed to save db configuration: %w", res.Error)
	}

	qualities := []models.AvailableQuality{
		{
			Name:       "240p",
			FolderName: "240p",
			Height:     240,
			Width:      426,
			Crf:        env.Hls240pCrf,

			VideoBitrate:   env.Hls240pVideoBitrate,
			AudioBitrate:   "64k",
			Profile:        "baseline",
			Level:          "1.3",
			CodecStringAVC: "avc1.42e00d",

			Type:       "hls",
			Muted:      true,
			OutputFile: "out.m3u8",
			Enabled:    *env.EncodeHls240p,
		},
		{
			Name:       "360p",
			FolderName: "360p",
			Height:     360,
			Width:      640,
			Crf:        env.Hls360pCrf,

			VideoBitrate:   env.Hls360pVideoBitrate,
			AudioBitrate:   "96k",
			Profile:        "main",
			Level:          "3.0",
			CodecStringAVC: "avc1.4d401e",

			Type:       "hls",
			Muted:      true,
			OutputFile: "out.m3u8",
			Enabled:    *env.EncodeHls360p,
		},
		{
			Name:       "480p",
			FolderName: "480p",
			Height:     480,
			Width:      854,
			Crf:        env.Hls480pCrf,

			VideoBitrate:   env.Hls480pVideoBitrate,
			AudioBitrate:   "128k",
			Profile:        "main",
			Level:          "3.1",
			CodecStringAVC: "avc1.4d401f",

			Type:       "hls",
			Muted:      true,
			OutputFile: "out.m3u8",
			Enabled:    *env.EncodeHls480p,
		},
		{
			Name:       "720p",
			FolderName: "720p",
			Height:     720,
			Width:      1280,
			Crf:        env.Hls720pCrf,

			VideoBitrate:   env.Hls720pVideoBitrate,
			AudioBitrate:   "192k",
			Profile:        "high",
			Level:          "4.0",
			CodecStringAVC: "avc1.64001f",

			Type:       "hls",
			Muted:      true,
			OutputFile: "out.m3u8",
			Enabled:    *env.EncodeHls720p,
		},
		{
			Name:       "1080p",
			FolderName: "1080p",
			Height:     1080,
			Width:      1920,
			Crf:        env.Hls1080pCrf,

			VideoBitrate:   env.Hls1080pVideoBitrate,
			AudioBitrate:   "256k",
			Profile:        "high",
			Level:          "4.2",
			CodecStringAVC: "avc1.64002a",

			Type:       "hls",
			Muted:      true,
			OutputFile: "out.m3u8",
			Enabled:    *env.EncodeHls1080p,
		},
		{
			Name:       "1440p",
			FolderName: "1440p",
			Height:     1440,
			Width:      2560,
			Crf:        env.Hls1440pCrf,

			VideoBitrate:   env.Hls1440pVideoBitrate,
			AudioBitrate:   "256k",
			Profile:        "high",
			Level:          "5.1",
			CodecStringAVC: "avc1.640033",

			Type:       "hls",
			Muted:      true,
			OutputFile: "out.m3u8",
			Enabled:    *env.EncodeHls1440p,
		},
		{
			Name:       "2160p",
			FolderName: "2160p",
			Height:     2160,
			Width:      3840,
			Crf:        env.Hls2160pCrf,

			VideoBitrate:   env.Hls2160pVideoBitrate,
			AudioBitrate:   "320k",
			Profile:        "high",
			Level:          "5.2",
			CodecStringAVC: "avc1.640034",

			Type:       "hls",
			Muted:      true,
			OutputFile: "out.m3u8",
			Enabled:    *env.EncodeHls2160p,
		},
	}
	return app.Snapshot{Config: env, Qualities: qualities}, nil
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
			log.Printf("Failed to parse bool from value %q; using default", *value)
		}
	}
	if defaultValue != nil && *defaultValue == true {
		if value != nil {
			*value = "true"
		}
	} else {
		if value != nil {
			*value = "false"
		}
	}
	return defaultValue
}

func getEnvDb_int64(value *string, defaultValue int64) int64 {
	if value != nil && *value != "" {
		res, err := strconv.ParseInt(*value, 10, 64)
		if err != nil {
			log.Printf("Failed to parse int from value %q; using default", *value)
			*value = fmt.Sprint(defaultValue)
			return defaultValue
		}
		return res
	}
	if value != nil {
		*value = fmt.Sprint(defaultValue)
	}
	return defaultValue
}
func getEnvDb_int(value *string, defaultValue int) int {
	if value != nil && *value != "" {
		res, err := strconv.Atoi(*value)
		if err != nil {
			log.Printf("Failed to parse int from value %q; using default", *value)
			*value = fmt.Sprint(defaultValue)
			return defaultValue
		}
		return res
	}
	if value != nil {
		*value = fmt.Sprint(defaultValue)
	}
	return defaultValue
}

func getEnvDb_float64(value *string, defaultValue float64) float64 {
	if value != nil && *value != "" {
		res, err := strconv.ParseFloat(*value, 64)
		if err != nil {
			log.Printf("Failed to parse float from value %q; using default", *value)
			*value = fmt.Sprintf("%v", defaultValue)
			return defaultValue
		}
		return res
	}
	if value != nil {
		*value = fmt.Sprintf("%v", defaultValue)
	}
	return defaultValue
}

func boolPtr(boolean bool) *bool {
	return &boolean
}
