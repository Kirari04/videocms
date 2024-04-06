package models

type SettingValidation struct {
	ID uint `validate:"required,number"`
	Setting
}

type Setting struct {
	Model
	AppName string `validate:"required,min=1,max=120" gorm:"size:120;"`

	Project              string `validate:"required,min=1,max=512" gorm:"size:512;"`
	ProjectDocumentation string `validate:"required,min=1,max=512" gorm:"size:512;"`
	ProjectDownload      string `validate:"required,min=1,max=512" gorm:"size:512;"`
	ProjectExampleVideo  string `validate:"required,min=1,max=512" gorm:"size:512;"`

	JwtSecretKey       string `validate:"required,min=8,max=512" gorm:"size:512;"`
	JwtUploadSecretKey string `validate:"required,min=8,max=512" gorm:"size:512;"`

	CookieDomain string `validate:"required,min=8,max=225" gorm:"size:225;"`

	ReloadHtml        string `validate:"required,boolean"`
	EncodingEnabled   string `validate:"required,boolean"`
	UploadEnabled     string `validate:"required,boolean"`
	RatelimitEnabled  string `validate:"required,boolean"`
	CloudflareEnabled string `validate:"required,boolean"`

	MaxItemsMultiDelete string `validate:"required,number,min=1"`
	MaxRunningEncodes   string `validate:"required,number,min=1"`

	MaxUploadFilesize   string `validate:"required,number,min=1"`
	MaxUploadChuncksize string `validate:"required,number,min=1"`
	MaxUploadSessions   string `validate:"required,number,min=1"`
	MaxPostSize         string `validate:"required,number,min=1"`

	CorsAllowOrigins     string `validate:"required,min=1,max=1000" gorm:"size:1000;"`
	CorsAllowHeaders     string `validate:"required,min=1,max=1000" gorm:"size:1000;"`
	CorsAllowCredentials string `validate:"required,boolean"`

	CaptchaEnabled               string `validate:"required,boolean"`
	CaptchaType                  string `validate:"required_if=CaptchaEnabled 1,omitempty,min=1,max=10" gorm:"size:10;"`
	Captcha_Recaptcha_PrivateKey string `validate:"required_if=CaptchaType recaptcha,omitempty,min=1,max=40" gorm:"size:40;"`
	Captcha_Recaptcha_PublicKey  string `validate:"required_if=CaptchaType recaptcha,omitempty,min=1,max=40" gorm:"size:40;"`
	Captcha_Hcaptcha_PrivateKey  string `validate:"required_if=CaptchaType hcaptcha,omitempty,min=1,max=42" gorm:"size:42;"`
	Captcha_Hcaptcha_PublicKey   string `validate:"required_if=CaptchaType hcaptcha,omitempty,uuid_rfc4122"`

	EncodeHls240p  string `validate:"required,boolean"`
	EncodeHls360p  string `validate:"required,boolean"`
	EncodeHls480p  string `validate:"required,boolean"`
	EncodeHls720p  string `validate:"required,boolean"`
	EncodeHls1080p string `validate:"required,boolean"`
	EncodeHls1440p string `validate:"required,boolean"`
	EncodeHls2160p string `validate:"required,boolean"`
	EncodeAv1      string `validate:"required,boolean"`
	EncodeVp9      string `validate:"required,boolean"`
	EncodeH264     string `validate:"required,boolean"`

	FFmpegAv1AudioCodec  string `validate:"required,min=1,max=40" gorm:"size:40;"`
	FFmpegVp9AudioCodec  string `validate:"required,min=1,max=40" gorm:"size:40;"`
	FFmpegH264AudioCodec string `validate:"required,min=1,max=40" gorm:"size:40;"`

	FFmpegAv1Crf  string `validate:"required,number,min=1,max=50"`
	FFmpegVp9Crf  string `validate:"required,number,min=1,max=50"`
	FFmpegH264Crf string `validate:"required,number,min=1,max=50"`

	FFmpegAv1Height  string `validate:"required,number,min=1"`
	FFmpegAv1Width   string `validate:"required,number,min=1"`
	FFmpegVp9Height  string `validate:"required,number,min=1"`
	FFmpegVp9Width   string `validate:"required,number,min=1"`
	FFmpegH264Height string `validate:"required,number,min=1"`
	FFmpegH264Width  string `validate:"required,number,min=1"`

	PluginPgsServer string `validate:"required"`
}
