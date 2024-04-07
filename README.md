# Video-CMS

This project is a cms for hosting your videos.

## Questions & Feature Requests

You can ask project related questions & feature requests on <a href="https://discord.gg/pHcstaPThK" target="_blank"> Discord </a>.

## Api-Doc

View the latest documentation on <a href="https://documenter.getpostman.com/view/15650779/2s93CPrY2w" target="_blank">Postman</a>

## Deploy

### Alpha

The Alpha image contains the latest code with the full api funtionallity.<br>
Deploy with docker: `docker run -p 3000:3000 kirari04/videocms:alpha` <br>

### Images For Arm64 CPU's Like Raspberry Pi

just use the same images like before just add `_arm64` to the end of the tag. <br>
Here some examples:

```bash
docker run -p 3000:3000 kirari04/videocms:alpha_arm64
```

### Frontend

Use the `panel` container for that:

```bash
docker run -p 3000:3000 kirari04/videocms:panel
```

## Authentication / Default Admin

Create a new user by running the command `./main.bin create:user` inside the container.

## Configuration

### Frontend

Those are the env configs for the `panel` image.

```bash
NODE_ENV=production
NUXT_PUBLIC_API_URL=https://videocms.senpai.one/api
NUXT_PUBLIC_BASE_URL=https://videocms.senpai.one
NUXT_PUBLIC_DOCKER_HUB_TAG=kirari04/videocms:alpha
NUXT_PUBLIC_API_DOCS=https://documenter.getpostman.com/view/15650779/2s93CPrY2w
NUXT_PUBLIC_TUTORIAL=https://videocms.tawk.help/category/tutorial
NUXT_PUBLIC_NAME=VideoCMS
NUXT_PUBLIC_DEMO=false
```

### API

When running `./main config` the following will be printed out:

```json
{
  "AppName": "VideoCMS",
  "Host": ":3000",
  "Project": "https://github.com/Kirari04/videocms",
  "ProjectDocumentation": "https://documenter.getpostman.com/view/15650779/2s93CPrY2w",
  "ProjectDownload": "https://documenter.getpostman.com/view/15650779/2s93CPrY2w",
  "JwtSecretKey": "secretkey",
  "ReloadHtml": false,
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


## Screenshots

### Simple Panel
![Alt text](./docs/image.png)

### Advanced File Information
![Alt text](./docs/image2.png)
![Alt text](./docs/image5.png)

### Easy Export
![Alt text](./docs/image3.png)
![Alt text](./docs/image4.png)

### Multiple Qualities
![Alt text](./docs/image6.png)

### Multiple Subtitles
![Alt text](./docs/image7.png)

### Multiple Audio Channels
![Alt text](./docs/image8.png)
