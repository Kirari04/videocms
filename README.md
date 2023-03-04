# Video-CMS

This project is a cms for hosting your videos.

## Api-Doc

View the latest documentation on <a href="https://documenter.getpostman.com/view/15650779/2s93CPrY2w" target="_blank">Postman</a>

## Deploy

### Alpha

The Alpha image contains the latest code with the full funtionallity.<br>
Deploy with docker: `docker run -p 3000:3000 kirari04/videocms:alpha-1` <br>

### Demo

The Demo image contains the latest code with limited functionallity. (encode & upload are disabled by default)<br>
Deploy with docker: `docker run -p 3000:3000 kirari04/videocms:demo` <br>
You can also use the live preview here: <a href="https://videocms.senpai.one/" target="_blank">videocms.senpai.one</a>

## Images for Arm CPU's like raspberry pi

just use the same images like before just add `_arm64` to the end of the tag:

```
docker run -p 3000:3000 kirari04/videocms:alpha-1_arm64

docker run -p 3000:3000 kirari04/videocms:demo_arm64
```

## Authentication / Default admin user

Default username & password are `admin` and `12345678`
