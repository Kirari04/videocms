package controllers

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

type CaptchaViewData struct {
	CaptchaType string
	CaptchaKey  string
	AppName     string
	UUID        string
	ReturnTo    string
	Error       string
}

func (h *Handlers) GetCaptchaChallenge(c echo.Context) error {
	uuid := c.QueryParam("uuid")
	if uuid == "" {
		return c.Redirect(http.StatusSeeOther, "/")
	}

	data := CaptchaViewData{
		CaptchaType: h.Config().CaptchaType,
		AppName:     h.Config().AppName,
		UUID:        uuid,
		ReturnTo:    safeCaptchaReturn(c.QueryParam("return")),
	}

	switch h.Config().CaptchaType {
	case "recaptcha":
		data.CaptchaKey = h.Config().Captcha_Recaptcha_PublicKey
	case "hcaptcha":
		data.CaptchaKey = h.Config().Captcha_Hcaptcha_PublicKey
	case "turnstile":
		data.CaptchaKey = h.Config().Captcha_Turnstile_PublicKey
	}

	return c.Render(http.StatusOK, "captcha.html", data)
}

func (h *Handlers) VerifyCaptchaChallenge(c echo.Context) error {
	uuid := c.FormValue("uuid")
	if uuid == "" {
		return c.Redirect(http.StatusSeeOther, "/")
	}
	returnTo := safeCaptchaReturn(c.FormValue("return"))

	valid, err := h.Auth.CaptchaValid(c)
	if err != nil || !valid {
		data := CaptchaViewData{
			CaptchaType: h.Config().CaptchaType,
			AppName:     h.Config().AppName,
			UUID:        uuid,
			ReturnTo:    returnTo,
			Error:       "Captcha verification failed. Please try again.",
		}
		switch h.Config().CaptchaType {
		case "recaptcha":
			data.CaptchaKey = h.Config().Captcha_Recaptcha_PublicKey
		case "hcaptcha":
			data.CaptchaKey = h.Config().Captcha_Hcaptcha_PublicKey
		case "turnstile":
			data.CaptchaKey = h.Config().Captcha_Turnstile_PublicKey
		}
		return c.Render(http.StatusBadRequest, "captcha.html", data)
	}

	// Generate JWT
	token, expiration, err := h.Auth.GenerateCaptchaJWT(c.RealIP())
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

	redirectPath := "/v/" + uuid
	if returnTo == "download" {
		redirectPath += "/download"
	}
	return c.Redirect(http.StatusSeeOther, redirectPath)
}

func safeCaptchaReturn(returnTo string) string {
	if returnTo == "download" {
		return returnTo
	}
	return ""
}
