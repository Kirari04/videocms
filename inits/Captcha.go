package inits

import (
	"ch/kirari04/videocms/config"

	"github.com/dpapathanasiou/go-recaptcha"
	"github.com/kirari04/go-hcaptcha"
)

func Captcha() {
	recaptcha.Init(config.ENV.Captcha_Recaptcha_PrivateKey)
	hcaptcha.Init(config.ENV.Captcha_Hcaptcha_PrivateKey)
}
