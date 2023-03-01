package config

import (
	"fmt"
	"os"
	"reflect"
)

type Config struct {
	AppName string
	Host    string
	Project string

	JwtSecretKey string
}

type ConfigMap map[string]string

var ENV Config

func Setup() {
	ENV.AppName = getEnv("AppName", "VideoCMS")
	ENV.Host = getEnv("Host", "127.0.0.1:3000")
	ENV.Project = "https://hub.docker.com/r/kirari04/videocms"

	ENV.JwtSecretKey = getEnv("JwtSecretKey", "secret")
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
