# Video-CMS

This project is a cms for hosting your videos.

## Questions & Feature Requests

You can ask project related questions & feature requests on <a href="https://videocms.tawk.help/" target="_blank"> Tawk </a>.

## Api-Doc

View the latest documentation on <a href="https://documenter.getpostman.com/view/15650779/2s93CPrY2w" target="_blank">Postman</a>

## Deploy

### Alpha

The Alpha image contains the latest code with the full funtionallity.<br>
Deploy with docker: `docker run -p 3000:3000 kirari04/videocms:alpha-1` <br>

### Panel

The Panel image contains the latest code with the full funtionallity and the frontend Panel.<br>
Deploy with docker: `docker run -p 3000:3000 kirari04/videocms:panel` <br>

### Demo

The Demo image contains the latest code with limited functionallity. (encode & upload are disabled by default)<br>
Deploy with docker: `docker run -p 3000:3000 kirari04/videocms:demo` <br>

### Demo Panel

The Demo Panel image contains the latest code with limited functionallity and the frontend Panel. (encode & upload are disabled by default)<br>
Deploy with docker: `docker run -p 3000:3000 kirari04/videocms:demo_panel` <br>
You can also use the live preview here: <a href="https://videocms.senpai.one/" target="_blank">videocms.senpai.one</a>

## Images For Arm64 CPU's Like Raspberry Pi

just use the same images like before just add `_arm64` to the end of the tag. <br>
Here some examples

```
docker run -p 3000:3000 kirari04/videocms:alpha-1_arm64
docker run -p 3000:3000 kirari04/videocms:panel_arm64
docker run -p 3000:3000 kirari04/videocms:demo_arm64
```

## Authentication / Default Admin

Default username & password are `admin` and `12345678`

## Configuration

```json
{
  "AppName": "VideoCMS",
  "Host": ":3000",
  "Project": "https://github.com/Kirari04/videocms",
  "ProjectDocumentation": "https://documenter.getpostman.com/view/15650779/2s93CPrY2w",
  "ProjectDownload": "https://documenter.getpostman.com/view/15650779/2s93CPrY2w",
  "JwtSecretKey": "secretkey",
  "ReloadHtml": false,
  "PanelEnabled": false,
  "EncodingEnabled": true,
  "UploadEnabled": true,
  "RatelimitEnabled": false,
  "CloudflareEnabled": false,
  "MaxItemsMultiDelete": 1000,
  "MaxRunningEncodes": 1,
  "MaxUploadFilesize": 5368709120,
  "MaxUploadChuncksize": 20971520,
  "MaxUploadSessions": 10,
  "MaxPostSize": 104857600,
  "FolderVideoQualitysPub": "/videos/qualitys",
  "FolderVideoQualitysPriv": "./videos/qualitys",
  "FolderVideoUploadsPriv": "./videos/uploads",
  "CorsAllowOrigins": "*",
  "CorsAllowHeaders": "*",
  "CorsAllowCredentials": true,
  "CaptchaEnabled": false,
  "CaptchaType": "",
  "Captcha_Recaptcha_PrivateKey": "",
  "Captcha_Recaptcha_PublicKey": "",
  "Captcha_Hcaptcha_PrivateKey": "",
  "Captcha_Hcaptcha_PublicKey": "",
  "EncodeHls240p": true,
  "EncodeHls360p": true,
  "EncodeHls480p": true,
  "EncodeHls720p": true,
  "EncodeHls1080p": true,
  "EncodeHls1440p": false,
  "EncodeHls2160p": false,
  "EncodeAv1": false,
  "EncodeVp9": false,
  "EncodeH264": true,
  "FFmpegAv1AudioCodec": "aac",
  "FFmpegVp9AudioCodec": "libopus",
  "FFmpegH264AudioCodec": "aac",
  "FFmpegAv1Crf": 30,
  "FFmpegVp9Crf": 30,
  "FFmpegH264Crf": 30,
  "FFmpegAv1Height": 480,
  "FFmpegAv1Width": 854,
  "FFmpegVp9Height": 480,
  "FFmpegVp9Width": 854,
  "FFmpegH264Height": 480,
  "FFmpegH264Width": 854
}
```
