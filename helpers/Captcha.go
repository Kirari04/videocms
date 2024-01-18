package helpers

import (
	"ch/kirari04/videocms/config"
	"errors"

	recaptcha "github.com/dpapathanasiou/go-recaptcha"
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
	}

	return false, errors.New("invalid CaptchaType set")
}

func recaptchaValidate(c echo.Context) (bool, error) {
	// parse & validate request
	type Validation struct {
		Token string `validate:"required,min=1,max=1500" json:"g-recaptcha-response" form:"g-recaptcha-response"`
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
		Token string `validate:"required,min=1,max=1500" json:"h-captcha-response" form:"h-captcha-response"`
	}
	var validation Validation
	if _, err := Validate(c, &validation); err != nil {
		return false, err
	}

	return hcaptcha.Confirm(c.RealIP(), validation.Token)
}
