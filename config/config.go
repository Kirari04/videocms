package config

import (
	"log"
	"os"
	"strconv"
)

var ENV Config
var EXTENSIONS []string = []string{
	"mp4",   // MPEG-4 Part 14, most common web format
	"mkv",   // Matroska Multimedia Container, high quality/multi-stream
	"webm",  // Royalty-free format for the web (VP8/VP9)
	"avi",   // Audio Video Interleave, legacy Windows format
	"mov",   // QuickTime File Format, Apple standard
	"ts",    // MPEG transport stream, common in broadcasting/streaming
	"m4v",   // Apple iTunes video format
	"3gp",   // Multimedia container for 3G mobile phones
	"3g2",   // Multimedia container for 3G2 mobile phones
	"wmv",   // Windows Media Video
	"mpg",   // MPEG-1/2 video format
	"mpeg",  // MPEG-1/2 video format
	"asf",   // Advanced Systems Format, Microsoft streaming format
	"flv",   // Flash Video
	"f4v",   // Flash Video (MPEG-4 based)
	"m2ts",  // Blu-ray Disc Audio-Video (BDAV) MPEG-2 Transport Stream
	"mts",   // AVCHD video format, common on camcorders
	"vob",   // DVD Video Object
	"mxf",   // Material Exchange Format, common in professional editing
	"divx",  // DivX video format
	"ogv",   // Ogg Video format
	"rm",    // RealMedia format
	"rmvb",  // RealMedia Variable Bitrate
	"dv",    // Digital Video format
}

var VERSION string = "v0.0.6"

type Config struct {
	Host string `validate:"required,min=1,max=120"`

	AppName string
	BaseUrl string

	Project              string
	ProjectDocumentation string
	ProjectDownload      string
	ProjectExampleVideo  string

	JwtSecretKey       string
	JwtUploadSecretKey string

	ReloadHtml        *bool
	EncodingEnabled   *bool
	UploadEnabled     *bool
	RatelimitEnabled  *bool
	CloudflareEnabled *bool

	MaxItemsMultiDelete int64
	MaxRunningEncodes   int64
	MaxFramerate        int64

	MaxUploadFilesize   int64
	MaxUploadChuncksize int64
	MaxUploadSessions   int64
	MaxPostSize         int64

	FolderVideoQualitysPub  string `validate:"required,min=1,max=255"`
	FolderVideoQualitysPriv string `validate:"required,min=1,max=255"`
	FolderVideoUploadsPriv  string `validate:"required,min=1,max=255"`

	CorsAllowOrigins     string
	CorsAllowHeaders     string
	CorsAllowCredentials *bool

	CaptchaEnabled               *bool
	CaptchaType                  string
	Captcha_Recaptcha_PrivateKey string
	Captcha_Recaptcha_PublicKey  string
	Captcha_Hcaptcha_PrivateKey  string
	Captcha_Hcaptcha_PublicKey   string

	EncodeHls240p        *bool
	Hls240pVideoBitrate  string
	Hls240pCrf           int
	EncodeHls360p        *bool
	Hls360pVideoBitrate  string
	Hls360pCrf           int
	EncodeHls480p        *bool
	Hls480pVideoBitrate  string
	Hls480pCrf           int
	EncodeHls720p        *bool
	Hls720pVideoBitrate  string
	Hls720pCrf           int
	EncodeHls1080p       *bool
	Hls1080pVideoBitrate string
	Hls1080pCrf          int
	EncodeHls1440p       *bool
	Hls1440pVideoBitrate string
	Hls1440pCrf          int
	EncodeHls2160p       *bool
	Hls2160pVideoBitrate string
	Hls2160pCrf          int

	PluginPgsServer       string
	EnablePluginPgsServer *bool

	StatsDriveName string `validate:"required,min=1,max=255"`

	ContinueWatchingPopupEnabled *bool

	DownloadEnabled *bool
}

type PublicConfig struct {
	AppName         string
	BaseUrl         string
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

	ContinueWatchingPopupEnabled bool

	DownloadEnabled bool
}

func (c Config) PublicConfig() PublicConfig {
	return PublicConfig{
		AppName:         c.AppName,
		BaseUrl:         c.BaseUrl,
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

		ContinueWatchingPopupEnabled: *c.ContinueWatchingPopupEnabled,

		DownloadEnabled: *c.DownloadEnabled,
	}
}

type ConfigMap map[string]string

func Setup() {
	ENV.Host = getEnv("Host", ":3000")

	ENV.FolderVideoQualitysPriv = getEnv("FolderVideoQualitysPriv", "./videos/qualitys")
	ENV.FolderVideoQualitysPub = getEnv("FolderVideoQualitysPub", "/videos/qualitys")
	ENV.FolderVideoUploadsPriv = getEnv("FolderVideoUploadsPriv", "./videos/uploads")
	ENV.StatsDriveName = getEnv("StatsDriveName", "nvme0n1")
}

// getters
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
