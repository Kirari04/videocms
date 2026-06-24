package auth

import (
	"errors"
	"fmt"
	"time"

	recaptcha "github.com/dpapathanasiou/go-recaptcha"
	"github.com/imroc/req/v3"
	hcaptcha "github.com/kirari04/go-hcaptcha"
	"github.com/labstack/echo/v4"
)

func (s *Service) CaptchaValid(c echo.Context) (bool, error) {
	cfg := s.Config()
	if cfg.CaptchaEnabled == nil || !*cfg.CaptchaEnabled {
		return true, nil
	}

	switch cfg.CaptchaType {
	case "recaptcha":
		return s.recaptchaValidate(c)
	case "hcaptcha":
		return s.hcaptchaValidate(c)
	case "turnstile":
		return s.turnstileValidate(c)
	}

	return false, errors.New("invalid CaptchaType set")
}

func (s *Service) recaptchaValidate(c echo.Context) (bool, error) {
	token, err := captchaToken(c, "g-recaptcha-response")
	if err != nil {
		return false, err
	}

	recaptcha.Init(s.Config().Captcha_Recaptcha_PrivateKey)
	return recaptcha.Confirm(c.RealIP(), token)
}

func (s *Service) hcaptchaValidate(c echo.Context) (bool, error) {
	token, err := captchaToken(c, "h-captcha-response")
	if err != nil {
		return false, err
	}

	hcaptcha.Init(s.Config().Captcha_Hcaptcha_PrivateKey)
	return hcaptcha.Confirm(c.RealIP(), token)
}

func (s *Service) turnstileValidate(c echo.Context) (bool, error) {
	token, err := captchaToken(c, "cf-turnstile-response")
	if err != nil {
		return false, err
	}

	client := req.C().SetTimeout(5 * time.Second)
	var result struct {
		Success bool `json:"success"`
	}

	resp, err := client.R().
		SetFormData(map[string]string{
			"secret":   s.Config().Captcha_Turnstile_PrivateKey,
			"response": token,
			"remoteip": c.RealIP(),
		}).
		SetSuccessResult(&result).
		Post("https://challenges.cloudflare.com/turnstile/v0/siteverify")

	if err != nil {
		return false, err
	}
	if !resp.IsSuccessState() {
		return false, errors.New("failed to validate turnstile")
	}

	return result.Success, nil
}

func captchaToken(c echo.Context, field string) (string, error) {
	token := c.FormValue(field)
	if token == "" {
		token = c.QueryParam(field)
	}
	if token == "" {
		var body map[string]string
		if err := c.Bind(&body); err == nil {
			token = body[field]
		}
	}
	if token == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if len(token) > 1500 {
		return "", fmt.Errorf("%s must be at most 1500 characters", field)
	}
	return token, nil
}
