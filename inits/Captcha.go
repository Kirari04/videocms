package inits

import (
	"ch/kirari04/videocms/config"

	"github.com/dpapathanasiou/go-recaptcha"
)

func Captcha() {
	recaptcha.Init(config.ENV.Captcha_Recaptcha_PrivateKey)
}
