package config

import (
	"fmt"
	"os"
	"reflect"
)

type Config struct {
	AppName string `validate:"required,min=1,max=120"`
	Host    string `validate:"required,min=1,max=120"`
	Project string `validate:"required,min=1,max=120"`

	JwtSecretKey string `validate:"required,min=8,max=512"`

	EncodingEnabled  string `validate:"required,boolean"`
	UploadEnabled    string `validate:"required,boolean"`
	RatelimitEnabled string `validate:"required,boolean"`
}

type ConfigMap map[string]string

var ENV Config

func Setup() {
	ENV.AppName = getEnv("AppName", "VideoCMS")
	ENV.Host = getEnv("Host", ":3000")
	ENV.Project = "/"

	ENV.JwtSecretKey = getEnv("JwtSecretKey", "secretkey")

	ENV.EncodingEnabled = getEnv("EncodingEnabled", "false")
	ENV.UploadEnabled = getEnv("UploadEnabled", "false")
	ENV.RatelimitEnabled = getEnv("RatelimitEnabled", "true")
}

func (conv Config) String() string {
	var envFile string
	v := reflect.ValueOf(conv)
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		name := t.Field(i).Name
		value := v.Field(i).Interface()
		envFile += fmt.Sprintf("%s=%v\n", name, value)
	}
	return envFile
}

func getEnv(key string, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return defaultValue
}
