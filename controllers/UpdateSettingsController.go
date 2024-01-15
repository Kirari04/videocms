package controllers

import (
	"ch/kirari04/videocms/configdb"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"log"

	"github.com/gofiber/fiber/v2"
)

func UpdateSettings(c *fiber.Ctx) error {

	// parse & validate request
	var validation models.SettingValidation
	if err := c.BodyParser(&validation); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(validation); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	if res := inits.DB.First(&models.Setting{}, validation.ID); res.Error != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Setting not found by id")
	}

	var setting models.Setting
	setting.ID = validation.ID
	setting.AppName = validation.AppName
	setting.Project = validation.Project
	setting.ProjectDocumentation = validation.ProjectDocumentation
	setting.ProjectDownload = validation.ProjectDownload
	setting.JwtSecretKey = validation.JwtSecretKey
	setting.JwtUploadSecretKey = validation.JwtUploadSecretKey
	setting.CookieDomain = validation.CookieDomain
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
	setting.EncodeHls360p = validation.EncodeHls360p
	setting.EncodeHls480p = validation.EncodeHls480p
	setting.EncodeHls720p = validation.EncodeHls720p
	setting.EncodeHls1080p = validation.EncodeHls1080p
	setting.EncodeHls1440p = validation.EncodeHls1440p
	setting.EncodeHls2160p = validation.EncodeHls2160p
	setting.EncodeAv1 = validation.EncodeAv1
	setting.EncodeVp9 = validation.EncodeVp9
	setting.EncodeH264 = validation.EncodeH264
	setting.FFmpegAv1AudioCodec = validation.FFmpegAv1AudioCodec
	setting.FFmpegVp9AudioCodec = validation.FFmpegVp9AudioCodec
	setting.FFmpegH264AudioCodec = validation.FFmpegH264AudioCodec
	setting.FFmpegAv1Crf = validation.FFmpegAv1Crf
	setting.FFmpegVp9Crf = validation.FFmpegVp9Crf
	setting.FFmpegH264Crf = validation.FFmpegH264Crf
	setting.FFmpegAv1Height = validation.FFmpegAv1Height
	setting.FFmpegAv1Width = validation.FFmpegAv1Width
	setting.FFmpegVp9Height = validation.FFmpegVp9Height
	setting.FFmpegVp9Width = validation.FFmpegVp9Width
	setting.FFmpegH264Height = validation.FFmpegH264Height
	setting.FFmpegH264Width = validation.FFmpegH264Width
	if res := inits.DB.Save(&setting); res.Error != nil {
		log.Fatalln("Failed to save settings", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	// reload config in background
	go func() {
		configdb.Setup()
	}()
	return c.Status(fiber.StatusOK).SendString("ok")
}
