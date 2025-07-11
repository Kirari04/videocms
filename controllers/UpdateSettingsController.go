package controllers

import (
	"ch/kirari04/videocms/configdb"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func UpdateSettings(c echo.Context) error {

	// parse & validate request
	var validation models.SettingValidation
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.String(status, err.Error())
	}

	if res := inits.DB.First(&models.Setting{}, validation.ID); res.Error != nil {
		return c.String(http.StatusBadRequest, "Setting not found by id")
	}

	var setting models.Setting
	setting.ID = validation.ID
	setting.AppName = validation.AppName
	setting.BaseUrl = validation.BaseUrl
	setting.Project = validation.Project
	setting.ProjectDocumentation = validation.ProjectDocumentation
	setting.ProjectDownload = validation.ProjectDownload
	setting.ProjectExampleVideo = validation.ProjectExampleVideo
	setting.JwtSecretKey = validation.JwtSecretKey
	setting.JwtUploadSecretKey = validation.JwtUploadSecretKey
	setting.ReloadHtml = validation.ReloadHtml
	setting.EncodingEnabled = validation.EncodingEnabled
	setting.UploadEnabled = validation.UploadEnabled
	setting.RatelimitEnabled = validation.RatelimitEnabled
	setting.CloudflareEnabled = validation.CloudflareEnabled
	setting.MaxItemsMultiDelete = validation.MaxItemsMultiDelete
	setting.MaxRunningEncodes = validation.MaxRunningEncodes
	setting.MaxUploadFilesize = validation.MaxUploadFilesize
	setting.MaxUploadChuncksize = validation.MaxUploadChuncksize
	setting.MaxUploadSessions = validation.MaxUploadSessions
	setting.MaxPostSize = validation.MaxPostSize
	setting.CorsAllowHeaders = validation.CorsAllowHeaders
	setting.CorsAllowOrigins = validation.CorsAllowOrigins
	setting.CorsAllowCredentials = validation.CorsAllowCredentials
	setting.CaptchaEnabled = validation.CaptchaEnabled
	setting.CaptchaType = validation.CaptchaType
	setting.Captcha_Recaptcha_PrivateKey = validation.Captcha_Recaptcha_PrivateKey
	setting.Captcha_Recaptcha_PublicKey = validation.Captcha_Recaptcha_PublicKey
	setting.Captcha_Hcaptcha_PrivateKey = validation.Captcha_Hcaptcha_PrivateKey
	setting.Captcha_Hcaptcha_PublicKey = validation.Captcha_Hcaptcha_PublicKey
	setting.EncodeHls240p = validation.EncodeHls240p
	setting.Hls240pVideoBitrate = validation.Hls240pVideoBitrate
	setting.EncodeHls360p = validation.EncodeHls360p
	setting.Hls360pVideoBitrate = validation.Hls360pVideoBitrate
	setting.EncodeHls480p = validation.EncodeHls480p
	setting.Hls480pVideoBitrate = validation.Hls480pVideoBitrate
	setting.EncodeHls720p = validation.EncodeHls720p
	setting.Hls720pVideoBitrate = validation.Hls720pVideoBitrate
	setting.EncodeHls1080p = validation.EncodeHls1080p
	setting.Hls1080pVideoBitrate = validation.Hls1080pVideoBitrate
	setting.EncodeHls1440p = validation.EncodeHls1440p
	setting.Hls1440pVideoBitrate = validation.Hls1440pVideoBitrate
	setting.EncodeHls2160p = validation.EncodeHls2160p
	setting.Hls2160pVideoBitrate = validation.Hls2160pVideoBitrate
	setting.PluginPgsServer = validation.PluginPgsServer
	setting.EnablePluginPgsServer = validation.EnablePluginPgsServer
	setting.DownloadEnabled = validation.DownloadEnabled

	if res := inits.DB.Save(&setting); res.Error != nil {
		log.Fatalln("Failed to save settings", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}
	// reload config in background
	go func() {
		configdb.Setup()
		log.Println("reloaded config")
	}()
	return c.String(http.StatusOK, "ok")
}
