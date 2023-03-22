package helpers

import (
	"ch/kirari04/videocms/config"
	"errors"

	"github.com/dpapathanasiou/go-recaptcha"
	"github.com/gofiber/fiber/v2"
)

func CaptchaValid(c *fiber.Ctx) (bool, error) {
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

func recaptchaValidate(c *fiber.Ctx) (bool, error) {
	// parse & validate request
	type Validation struct {
		Token string `validate:"required,min=1,max=500" json:"g-captcha-response" form:"g-captcha-response"`
	}
	var validation Validation
	if err := c.BodyParser(&validation); err != nil {
		return false, errors.New("invalid body request format")
	}

	if errorsRes := ValidateStruct(validation); len(errorsRes) > 0 {
		return false, errors.New(errorsRes[0].Value)
	}

	return recaptcha.Confirm(c.IP(), validation.Token)
}

func hcaptchaValidate(c *fiber.Ctx) (bool, error) {
	// parse & validate request
	type Validation struct {
		Token string `validate:"required,min=1,max=500" json:"h-captcha-response" form:"h-captcha-response"`
	}
	var validation Validation
	if err := c.BodyParser(&validation); err != nil {
		return false, errors.New("invalid body request format")
	}

	if errorsRes := ValidateStruct(validation); len(errorsRes) > 0 {
		return false, errors.New(errorsRes[0].Value)
	}

	return recaptcha.Confirm(c.IP(), validation.Token)
}
