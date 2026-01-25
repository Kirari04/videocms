package controllers

import (
	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"net/http"

	"github.com/labstack/echo/v4"
)

type CaptchaViewData struct {
	CaptchaType string
	CaptchaKey  string
	UUID        string
	Error       string
}

func GetCaptchaChallenge(c echo.Context) error {
	uuid := c.QueryParam("uuid")
	if uuid == "" {
		return c.Redirect(http.StatusSeeOther, "/")
	}

	data := CaptchaViewData{
		CaptchaType: config.ENV.CaptchaType,
		UUID:        uuid,
	}

	switch config.ENV.CaptchaType {
	case "recaptcha":
		data.CaptchaKey = config.ENV.Captcha_Recaptcha_PublicKey
	case "hcaptcha":
		data.CaptchaKey = config.ENV.Captcha_Hcaptcha_PublicKey
	case "turnstile":
		data.CaptchaKey = config.ENV.Captcha_Turnstile_PublicKey
	}

	return c.Render(http.StatusOK, "captcha.html", data)
}

func VerifyCaptchaChallenge(c echo.Context) error {
	uuid := c.FormValue("uuid")
	if uuid == "" {
		return c.Redirect(http.StatusSeeOther, "/")
	}

	valid, err := helpers.CaptchaValid(c)
	if err != nil || !valid {
		data := CaptchaViewData{
			CaptchaType: config.ENV.CaptchaType,
			UUID:        uuid,
			Error:       "Captcha verification failed. Please try again.",
		}
		switch config.ENV.CaptchaType {
		case "recaptcha":
			data.CaptchaKey = config.ENV.Captcha_Recaptcha_PublicKey
		case "hcaptcha":
			data.CaptchaKey = config.ENV.Captcha_Hcaptcha_PublicKey
		case "turnstile":
			data.CaptchaKey = config.ENV.Captcha_Turnstile_PublicKey
		}
		return c.Render(http.StatusBadRequest, "captcha.html", data)
	}

	// Generate JWT
	token, expiration, err := auth.GenerateCaptchaJWT(c.RealIP())
	if err != nil {
		return c.String(http.StatusInternalServerError, "Failed to generate token")
	}

	// Set Cookie
	cookie := new(http.Cookie)
	cookie.Name = "captcha_bypass"
	cookie.Value = token
	cookie.Expires = expiration
	cookie.Path = "/"
	cookie.HttpOnly = true
	c.SetCookie(cookie)

	return c.Redirect(http.StatusSeeOther, "/v/"+uuid)
}
