package models

type SettingValidation struct {
	ID uint `validate:"required,number"`
	Setting
}

type Setting struct {
	Model
	AppName string `validate:"required,min=1,max=120" gorm:"size:120;"`
	BaseUrl string `validate:"required,min=1,max=255" gorm:"size:255;"`

	Project              string `validate:"required,min=1,max=512" gorm:"size:512;"`
	ProjectDocumentation string `validate:"required,min=1,max=512" gorm:"size:512;"`
	ProjectDownload      string `validate:"required,min=1,max=512" gorm:"size:512;"`
	ProjectExampleVideo  string `validate:"required,min=1,max=512" gorm:"size:512;"`

	JwtSecretKey       string `validate:"required,min=8,max=512" gorm:"size:512;"`
	JwtUploadSecretKey string `validate:"required,min=8,max=512" gorm:"size:512;"`

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

	EncodeHls240p        string `validate:"required,boolean"`
	Hls240pVideoBitrate  string `validate:"required,min=1,max=7" gorm:"size:7;"`
	EncodeHls360p        string `validate:"required,boolean"`
	Hls360pVideoBitrate  string `validate:"required,min=1,max=7" gorm:"size:7;"`
	EncodeHls480p        string `validate:"required,boolean"`
	Hls480pVideoBitrate  string `validate:"required,min=1,max=7" gorm:"size:7;"`
	EncodeHls720p        string `validate:"required,boolean"`
	Hls720pVideoBitrate  string `validate:"required,min=1,max=7" gorm:"size:7;"`
	EncodeHls1080p       string `validate:"required,boolean"`
	Hls1080pVideoBitrate string `validate:"required,min=1,max=7" gorm:"size:7;"`
	EncodeHls1440p       string `validate:"required,boolean"`
	Hls1440pVideoBitrate string `validate:"required,min=1,max=7" gorm:"size:7;"`
	EncodeHls2160p       string `validate:"required,boolean"`
	Hls2160pVideoBitrate string `validate:"required,min=1,max=7" gorm:"size:7;"`

	PluginPgsServer       string `validate:"required"`
	EnablePluginPgsServer string `validate:"required,boolean"`

	DownloadEnabled              string `validate:"required,boolean"`
	ContinueWatchingPopupEnabled string `validate:"required,boolean"`
}
