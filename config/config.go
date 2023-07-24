package config

import (
	"ch/kirari04/videocms/models"
	"log"
	"os"
	"strconv"
)

type Config struct {
	AppName string `validate:"required,min=1,max=120"`
	Host    string `validate:"required,min=1,max=120"`

	Project              string `validate:"required,min=1,max=512"`
	ProjectDocumentation string `validate:"required,min=1,max=512"`
	ProjectDownload      string `validate:"required,min=1,max=512"`

	JwtSecretKey string `validate:"required,min=8,max=512"`

	ReloadHtml        *bool `validate:"required,boolean"`
	PanelEnabled      *bool `validate:"required,boolean"`
	EncodingEnabled   *bool `validate:"required,boolean"`
	UploadEnabled     *bool `validate:"required,boolean"`
	RatelimitEnabled  *bool `validate:"required,boolean"`
	CloudflareEnabled *bool `validate:"required,boolean"`

	MaxItemsMultiDelete int64 `validate:"required,number,min=1"`
	MaxRunningEncodes   int64 `validate:"required,number,min=1"`

	MaxUploadFilesize   int64 `validate:"required,number,min=1"`
	MaxUploadChuncksize int64 `validate:"required,number,min=1"`
	MaxUploadSessions   int64 `validate:"required,number,min=1"`
	MaxPostSize         int64 `validate:"required,number,min=1"`

	FolderVideoQualitysPub  string `validate:"required,min=1,max=255"`
	FolderVideoQualitysPriv string `validate:"required,min=1,max=255"`
	FolderVideoUploadsPriv  string `validate:"required,min=1,max=255"`

	CorsAllowOrigins     string `validate:"required,min=1"`
	CorsAllowHeaders     string `validate:"required,min=1"`
	CorsAllowCredentials *bool  `validate:"required,boolean"`

	CaptchaEnabled               *bool  `validate:"required,boolean"`
	CaptchaType                  string `validate:"required_if=CaptchaEnabled 1,omitempty,min=1,max=10"`
	Captcha_Recaptcha_PrivateKey string `validate:"required_if=CaptchaType recaptcha,omitempty,min=1,max=40"`
	Captcha_Recaptcha_PublicKey  string `validate:"required_if=CaptchaType recaptcha,omitempty,min=1,max=40"`
	Captcha_Hcaptcha_PrivateKey  string `validate:"required_if=CaptchaType hcaptcha,omitempty,min=1,max=42"`
	Captcha_Hcaptcha_PublicKey   string `validate:"required_if=CaptchaType hcaptcha,omitempty,uuid_rfc4122"`

	EncodeHls240p  *bool `validate:"required,boolean"`
	EncodeHls360p  *bool `validate:"required,boolean"`
	EncodeHls480p  *bool `validate:"required,boolean"`
	EncodeHls720p  *bool `validate:"required,boolean"`
	EncodeHls1080p *bool `validate:"required,boolean"`
	EncodeHls1440p *bool `validate:"required,boolean"`
	EncodeHls2160p *bool `validate:"required,boolean"`
	EncodeAv1      *bool `validate:"required,boolean"`
	EncodeVp9      *bool `validate:"required,boolean"`
	EncodeH264     *bool `validate:"required,boolean"`

	FFmpegAv1AudioCodec  string `validate:"required,min=1"`
	FFmpegVp9AudioCodec  string `validate:"required,min=1"`
	FFmpegH264AudioCodec string `validate:"required,min=1"`

	FFmpegAv1Crf  int `validate:"required,number,min=1,max=50"`
	FFmpegVp9Crf  int `validate:"required,number,min=1,max=50"`
	FFmpegH264Crf int `validate:"required,number,min=1,max=50"`

	FFmpegAv1Height  int64 `validate:"required,number,min=1"`
	FFmpegAv1Width   int64 `validate:"required,number,min=1"`
	FFmpegVp9Height  int64 `validate:"required,number,min=1"`
	FFmpegVp9Width   int64 `validate:"required,number,min=1"`
	FFmpegH264Height int64 `validate:"required,number,min=1"`
	FFmpegH264Width  int64 `validate:"required,number,min=1"`
}

type PublicConfig struct {
	AppName         string
	Project         string
	EncodingEnabled bool
	UploadEnabled   bool

	MaxUploadFilesize   int64
	MaxUploadChuncksize int64
	MaxUploadSessions   int64

	FolderVideoQualitys string

	CaptchaEnabled              bool
	CaptchaType                 string
	Captcha_Recaptcha_PublicKey string
	Captcha_Hcaptcha_PublicKey  string
}

func (c Config) PublicConfig() PublicConfig {
	return PublicConfig{
		AppName:         c.AppName,
		Project:         c.Project,
		EncodingEnabled: *c.EncodingEnabled,
		UploadEnabled:   *c.UploadEnabled,

		MaxUploadFilesize:   c.MaxUploadFilesize,
		MaxUploadChuncksize: c.MaxUploadChuncksize,
		MaxUploadSessions:   c.MaxUploadSessions,

		FolderVideoQualitys: c.FolderVideoQualitysPub,

		CaptchaEnabled:              *c.CaptchaEnabled,
		CaptchaType:                 c.CaptchaType,
		Captcha_Recaptcha_PublicKey: c.Captcha_Recaptcha_PublicKey,
		Captcha_Hcaptcha_PublicKey:  c.Captcha_Hcaptcha_PublicKey,
	}
}

type ConfigMap map[string]string

var ENV Config

func Setup() {
	ENV.AppName = getEnv("AppName", "VideoCMS")
	ENV.Host = getEnv("Host", ":3000")

	ENV.Project = getEnv("Project", "https://github.com/Kirari04/videocms")
	ENV.ProjectDocumentation = getEnv("ProjectDocumentation", "https://documenter.getpostman.com/view/15650779/2s93CPrY2w")
	ENV.ProjectDownload = getEnv("ProjectDownload", "https://documenter.getpostman.com/view/15650779/2s93CPrY2w")

	ENV.JwtSecretKey = getEnv("JwtSecretKey", "secretkey")

	ENV.ReloadHtml = getEnv_bool("ReloadHtml", boolPtr(false))
	ENV.PanelEnabled = getEnv_bool("PanelEnabled", boolPtr(false))
	ENV.EncodingEnabled = getEnv_bool("EncodingEnabled", boolPtr(false))
	ENV.UploadEnabled = getEnv_bool("UploadEnabled", boolPtr(false))
	ENV.RatelimitEnabled = getEnv_bool("RatelimitEnabled", boolPtr(true))
	ENV.CloudflareEnabled = getEnv_bool("CloudflareEnabled", boolPtr(false))

	ENV.MaxItemsMultiDelete = getEnv_int64("MaxItemsMultiDelete", 1000)
	ENV.MaxRunningEncodes = getEnv_int64("MaxRunningEncodes", 1)

	ENV.MaxUploadFilesize = getEnv_int64("MaxUploadFilesize", 5*1024*1024*1024) // 5gb
	ENV.MaxUploadChuncksize = getEnv_int64("MaxUploadChuncksize", 20*1024*1024) // 20mb
	ENV.MaxUploadSessions = getEnv_int64("MaxUploadSessions", 2)
	ENV.MaxPostSize = getEnv_int64("MaxPostSize", 100*1024*1024) // 100mb

	ENV.FolderVideoQualitysPriv = getEnv("FolderVideoQualitysPriv", "./videos/qualitys")
	ENV.FolderVideoQualitysPub = getEnv("FolderVideoQualitysPub", "/videos/qualitys")
	ENV.FolderVideoUploadsPriv = getEnv("FolderVideoUploadsPriv", "./videos/uploads")

	ENV.CorsAllowHeaders = getEnv("CorsAllowHeaders", "*")
	ENV.CorsAllowOrigins = getEnv("CorsAllowOrigins", "*")
	ENV.CorsAllowCredentials = getEnv_bool("CorsAllowCredentials", boolPtr(true))

	ENV.CaptchaEnabled = getEnv_bool("CaptchaEnabled", boolPtr(false))
	ENV.CaptchaType = getEnv("CaptchaType", "")
	ENV.Captcha_Recaptcha_PrivateKey = getEnv("Captcha_Recaptcha_PrivateKey", "")
	ENV.Captcha_Recaptcha_PublicKey = getEnv("Captcha_Recaptcha_PublicKey", "")
	ENV.Captcha_Hcaptcha_PrivateKey = getEnv("Captcha_Hcaptcha_PrivateKey", "")
	ENV.Captcha_Hcaptcha_PublicKey = getEnv("Captcha_Hcaptcha_PublicKey", "")

	ENV.EncodeHls240p = getEnv_bool("EncodeHls240p", boolPtr(true))
	ENV.EncodeHls360p = getEnv_bool("EncodeHls360p", boolPtr(true))
	ENV.EncodeHls480p = getEnv_bool("EncodeHls480p", boolPtr(true))
	ENV.EncodeHls720p = getEnv_bool("EncodeHls720p", boolPtr(true))
	ENV.EncodeHls1080p = getEnv_bool("EncodeHls1080p", boolPtr(true))
	ENV.EncodeHls1440p = getEnv_bool("EncodeHls1440p", boolPtr(false))
	ENV.EncodeHls2160p = getEnv_bool("EncodeHls2160p", boolPtr(false))
	ENV.EncodeAv1 = getEnv_bool("EncodeAv1", boolPtr(false))
	ENV.EncodeVp9 = getEnv_bool("EncodeVp9", boolPtr(false))
	ENV.EncodeH264 = getEnv_bool("EncodeH264", boolPtr(true))

	ENV.FFmpegAv1AudioCodec = getEnv("FFmpegAv1AudioCodec", "aac")
	ENV.FFmpegVp9AudioCodec = getEnv("FFmpegVp9AudioCodec", "libopus")
	ENV.FFmpegH264AudioCodec = getEnv("FFmpegH264AudioCodec", "aac")

	ENV.FFmpegAv1Crf = getEnv_int("FFmpegAv1Crf", 30)
	ENV.FFmpegVp9Crf = getEnv_int("FFmpegVp9Crf", 30)
	ENV.FFmpegH264Crf = getEnv_int("FFmpegH264Crf", 30)

	ENV.FFmpegAv1Height = getEnv_int64("FFmpegAv1Height", 480)
	ENV.FFmpegAv1Width = getEnv_int64("FFmpegAv1Width", 854)
	ENV.FFmpegVp9Height = getEnv_int64("FFmpegVp9Height", 480)
	ENV.FFmpegVp9Width = getEnv_int64("FFmpegVp9Width", 854)
	ENV.FFmpegH264Height = getEnv_int64("FFmpegH264Height", 480)
	ENV.FFmpegH264Width = getEnv_int64("FFmpegH264Width", 854)

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
			Enabled:    *ENV.EncodeHls240p,
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
			Enabled:    *ENV.EncodeHls360p,
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
			Enabled:    *ENV.EncodeHls480p,
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
			Enabled:    *ENV.EncodeHls720p,
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
			Enabled:    *ENV.EncodeHls1080p,
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
			Enabled:    *ENV.EncodeHls1440p,
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
			Enabled:    *ENV.EncodeHls2160p,
		},
		{
			Name:       "av1",
			FolderName: "av1",
			Height:     ENV.FFmpegAv1Height,
			Width:      ENV.FFmpegAv1Width,
			Crf:        ENV.FFmpegAv1Crf,
			Type:       "av1",
			Muted:      false,
			AudioCodec: ENV.FFmpegAv1AudioCodec,
			OutputFile: "out.mp4",
			Enabled:    *ENV.EncodeAv1,
		},
		{
			Name:       "vp9",
			FolderName: "vp9",
			Height:     ENV.FFmpegVp9Height,
			Width:      ENV.FFmpegVp9Width,
			Crf:        ENV.FFmpegVp9Crf,
			Type:       "vp9",
			Muted:      false,
			AudioCodec: ENV.FFmpegVp9AudioCodec,
			OutputFile: "out.webm",
			Enabled:    *ENV.EncodeVp9,
		},
		{
			Name:       "h264",
			FolderName: "h264",
			Height:     ENV.FFmpegH264Height,
			Width:      ENV.FFmpegH264Width,
			Crf:        ENV.FFmpegH264Crf,
			Type:       "h264",
			Muted:      false,
			AudioCodec: ENV.FFmpegH264AudioCodec,
			OutputFile: "out.mp4",
			Enabled:    *ENV.EncodeH264,
		},
	}
}

func getEnv(key string, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return defaultValue
}

func getEnv_bool(key string, defaultValue *bool) *bool {
	if value := os.Getenv(key); value != "" {
		switch value {
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

	return defaultValue
}

func getEnv_int64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		res, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			log.Panicf("Failed to parse int from value %v", value)
		}
		return res
	}

	return defaultValue
}
func getEnv_int(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		res, err := strconv.Atoi(value)
		if err != nil {
			log.Panicf("Failed to parse int from value %v", value)
		}
		return res
	}

	return defaultValue
}

func boolPtr(boolean bool) *bool {
	return &boolean
}
