package controllers

import (
	"ch/kirari04/videocms/models"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *Handlers) GetSettings(c echo.Context) error {
	_, ok := c.Get("UserID").(uint)
	if !ok {
		log.Println("Failed to catch user")
		return c.NoContent(http.StatusInternalServerError)
	}

	var setting models.Setting
	if res := h.Deps.DB.FirstOrCreate(&setting); res.Error != nil {
		log.Println("Failed to get settings", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}
	response := struct {
		models.Setting
		RecaptchaConfigured bool `json:"Captcha_Recaptcha_PrivateKeyConfigured"`
		HcaptchaConfigured  bool `json:"Captcha_Hcaptcha_PrivateKeyConfigured"`
		TurnstileConfigured bool `json:"Captcha_Turnstile_PrivateKeyConfigured"`
	}{setting, setting.Captcha_Recaptcha_PrivateKey != "", setting.Captcha_Hcaptcha_PrivateKey != "", setting.Captcha_Turnstile_PrivateKey != ""}
	response.Captcha_Recaptcha_PrivateKey = ""
	response.Captcha_Hcaptcha_PrivateKey = ""
	response.Captcha_Turnstile_PrivateKey = ""
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.JSON(http.StatusOK, &response)
}
