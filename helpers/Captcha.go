package helpers

import (
	"ch/kirari04/videocms/config"
	"errors"
	"time"

	recaptcha "github.com/dpapathanasiou/go-recaptcha"
	"github.com/imroc/req/v3"
	hcaptcha "github.com/kirari04/go-hcaptcha"
	"github.com/labstack/echo/v4"
)

func CaptchaValid(c echo.Context) (bool, error) {
	if !*config.ENV.CaptchaEnabled {
		return true, nil
	}

	switch config.ENV.CaptchaType {
	case "recaptcha":
		return recaptchaValidate(c)
	case "hcaptcha":
		return hcaptchaValidate(c)
	case "turnstile":
		return turnstileValidate(c)
	}

	return false, errors.New("invalid CaptchaType set")
}

func recaptchaValidate(c echo.Context) (bool, error) {
	// parse & validate request
	type Validation struct {
		Token string `validate:"required,min=1,max=1500" json:"g-recaptcha-response" form:"g-recaptcha-response" query:"g-recaptcha-response"`
	}
	var validation Validation
	if _, err := Validate(c, &validation); err != nil {
		return false, err
	}

	return recaptcha.Confirm(c.RealIP(), validation.Token)
}

func hcaptchaValidate(c echo.Context) (bool, error) {
	// parse & validate request
	type Validation struct {
		Token string `validate:"required,min=1,max=1500" json:"h-captcha-response" form:"h-captcha-response" query:"h-captcha-response"`
	}
	var validation Validation
	if _, err := Validate(c, &validation); err != nil {
		return false, err
	}

	return hcaptcha.Confirm(c.RealIP(), validation.Token)
}

func turnstileValidate(c echo.Context) (bool, error) {
	// parse & validate request
	type Validation struct {
		Token string `validate:"required,min=1,max=1500" json:"cf-turnstile-response" form:"cf-turnstile-response" query:"cf-turnstile-response"`
	}
	var validation Validation
	if _, err := Validate(c, &validation); err != nil {
		return false, err
	}

	client := req.C().SetTimeout(5 * time.Second)
	var result struct {
		Success bool `json:"success"`
	}

	resp, err := client.R().
		SetFormData(map[string]string{
			"secret":   config.ENV.Captcha_Turnstile_PrivateKey,
			"response": validation.Token,
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
