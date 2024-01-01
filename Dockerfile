FROM alpine:3.14

WORKDIR /app
VOLUME /app/videos
VOLUME /app/public
VOLUME /app/database

RUN apk add --no-cache ffmpeg bash

COPY ./build/cmd/main_linux_amd64.bin ./
RUN mv ./main_linux_amd64.bin ./main.bin

COPY ./views ./views/
COPY ./public ./public/

ENV AppName=VideoCMS
ENV Host=:3000
ENV JwtSecretKey=secretkey
ENV EncodingEnabled=true
ENV UploadEnabled=true
ENV RatelimitEnabled=false
ENV CloudflareEnabled=false
ENV MaxItemsMultiDelete=1000
ENV MaxRunningEncodes=1
ENV MaxRunningEncodes_sub=1
ENV MaxRunningEncodes_audio=1

EXPOSE 3000

CMD ["./main.bin", "serve:main"]