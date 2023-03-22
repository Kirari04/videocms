package config

import (
	"encoding/json"
	"log"
	"os"
	"strconv"
)

type Config struct {
	AppName string `validate:"required,min=1,max=120"`
	Host    string `validate:"required,min=1,max=120"`
	Project string `validate:"required,min=1,max=120"`

	JwtSecretKey string `validate:"required,min=8,max=512"`

	PanelEnabled      *bool `validate:"required,boolean"`
	EncodingEnabled   *bool `validate:"required,boolean"`
	UploadEnabled     *bool `validate:"required,boolean"`
	RatelimitEnabled  *bool `validate:"required,boolean"`
	CloudflareEnabled *bool `validate:"required,boolean"`

	MaxItemsMultiDelete     int64 `validate:"required,number,min=1"`
	MaxRunningEncodes       int64 `validate:"required,number,min=1"`
	MaxRunningEncodes_sub   int64 `validate:"required,number,min=1"`
	MaxRunningEncodes_audio int64 `validate:"required,number,min=1"`

	CaptchaEnabled               *bool  `validate:"required,boolean"`
	CaptchaType                  string `validate:"required_if=CaptchaEnabled 1,omitempty,min=1,max=10"`
	Captcha_Recaptcha_PrivateKey string `validate:"required_if=CaptchaType recaptcha,omitempty,min=1,max=40"`
	Captcha_Recaptcha_PublicKey  string `validate:"required_if=CaptchaType recaptcha,omitempty,min=1,max=40"`
	Captcha_Hcaptcha_PrivateKey  string `validate:"required_if=CaptchaType hcaptcha,omitempty,min=1,max=42"`
	Captcha_Hcaptcha_PublicKey   string `validate:"required_if=CaptchaType hcaptcha,omitempty,uuid_rfc4122"`
}

type ConfigMap map[string]string

var ENV Config

func Setup() {
	ENV.AppName = getEnv("AppName", "VideoCMS")
	ENV.Host = getEnv("Host", ":3000")
	ENV.Project = "/"

	ENV.JwtSecretKey = getEnv("JwtSecretKey", "secretkey")

	ENV.PanelEnabled = getEnv_bool("PanelEnabled", boolPtr(false))
	ENV.EncodingEnabled = getEnv_bool("EncodingEnabled", boolPtr(false))
	ENV.UploadEnabled = getEnv_bool("UploadEnabled", boolPtr(false))
	ENV.RatelimitEnabled = getEnv_bool("RatelimitEnabled", boolPtr(true))
	ENV.CloudflareEnabled = getEnv_bool("CloudflareEnabled", boolPtr(false))

	ENV.MaxItemsMultiDelete = getEnv_int64("MaxItemsMultiDelete", 1000)
	ENV.MaxRunningEncodes = getEnv_int64("MaxRunningEncodes", 1)
	ENV.MaxRunningEncodes_sub = getEnv_int64("MaxRunningEncodes_sub", 1)
	ENV.MaxRunningEncodes_audio = getEnv_int64("MaxRunningEncodes_audio", 1)

	ENV.CaptchaEnabled = getEnv_bool("CaptchaEnabled", boolPtr(false))
	ENV.CaptchaType = getEnv("CaptchaType", "")
	ENV.Captcha_Recaptcha_PrivateKey = getEnv("Captcha_Recaptcha_PrivateKey", "")
	ENV.Captcha_Recaptcha_PublicKey = getEnv("Captcha_Recaptcha_PublicKey", "")
	ENV.Captcha_Hcaptcha_PrivateKey = getEnv("Captcha_Hcaptcha_PrivateKey", "")
	ENV.Captcha_Hcaptcha_PublicKey = getEnv("Captcha_Hcaptcha_PublicKey", "")

	if jsonString, err := json.Marshal(ENV); err == nil {
		log.Println(string(jsonString))
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

func boolPtr(boolean bool) *bool {
	return &boolean
}
