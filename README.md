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
just use the same images like before just add  `_arm64` to the end of the tag. <br>
Here some examples
```
docker run -p 3000:3000 kirari04/videocms:alpha-1_arm64
docker run -p 3000:3000 kirari04/videocms:panel_arm64
docker run -p 3000:3000 kirari04/videocms:demo_arm64
```

## Authentication / Default Admin
Default username & password are `admin` and `12345678`

## Configuration
```
AppName=VideoCMS
Host=:3000
Project=/
JwtSecretKey=secretkey
PanelEnabled=false
EncodingEnabled=false
UploadEnabled=false
RatelimitEnabled=true
```

## Example

### Clone / Hash Upload

```html
<script src="https://cdn.jsdelivr.net/npm/sjcl/sjcl.min.js"></script>
<script>
    // Assume 'bigBlobFile' is your big blob file
    let hash = sjcl.hash.sha256.hash(bigBlobFile);
    let hexHash = sjcl.codec.hex.fromBits(hash);
</script>
```