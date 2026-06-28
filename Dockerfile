# syntax=docker/dockerfile:1.7

ARG ALPINE_VERSION=3.22
ARG BUN_VERSION=1.2.18
ARG GO_VERSION=1.25.5

FROM --platform=$BUILDPLATFORM oven/bun:${BUN_VERSION} AS frontend_build

WORKDIR /app

ARG CHANNEL=beta
ARG DOCKER_IMAGE_TAG=kirari04/videocms:beta

COPY videocms-frontend/package.json videocms-frontend/bun.lock ./
RUN bun install --frozen-lockfile

COPY videocms-frontend/ .

RUN printf '%s\n' \
    'NUXT_PUBLIC_API_URL=/api' \
    'NUXT_PUBLIC_BASE_URL=' \
    "NUXT_PUBLIC_DOCKER_HUB_TAG=${DOCKER_IMAGE_TAG}" \
    'NUXT_PUBLIC_NAME=VideoCMS' \
    'NUXT_PUBLIC_DEMO=false' \
    "NUXT_PUBLIC_RELEASE_CHANNEL=${CHANNEL}" \
    > .env

RUN bun run generate

FROM --platform=$TARGETPLATFORM golang:${GO_VERSION} AS go_build

WORKDIR /build

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=v0.0.0-dev
ARG CHANNEL=dev

COPY go.mod go.sum ./
RUN go mod download

# Copy source code after dependencies so stale go.mod/go.sum is caught by CI.
COPY . .

RUN mkdir -p /out && \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build \
    -ldflags "-linkmode external -extldflags -static -X ch/kirari04/videocms/config.VERSION=${VERSION}" \
    -a -installsuffix cgo \
    -o /out/videocms \
    ./main.go

FROM go_build AS sbom

RUN curl -sSfL https://raw.githubusercontent.com/anchore/syft/main/install.sh | sh -s -- -b /go/bin

RUN /go/bin/syft packages . -o spdx-json=/out/sbom.spdx.json

FROM scratch AS binary

COPY --from=go_build /out/videocms /videocms

FROM --platform=$TARGETPLATFORM alpine:${ALPINE_VERSION}

WORKDIR /app

RUN apk add --no-cache ffmpeg bash

ARG VERSION=v0.0.0-dev
ARG CHANNEL=dev
ARG DOCKER_IMAGE_TAG=kirari04/videocms:dev

LABEL org.opencontainers.image.title="VideoCMS" \
      org.opencontainers.image.description="Self-hosted video content management system" \
      org.opencontainers.image.source="https://github.com/Kirari04/videocms" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.ref.name="${DOCKER_IMAGE_TAG}" \
      ch.kirari04.videocms.channel="${CHANNEL}"

COPY --from=go_build /out/videocms ./main.bin
COPY --from=sbom /out/sbom.spdx.json /app/sbom.spdx.json

COPY ./views ./views/
COPY ./public ./public/
COPY --from=frontend_build /app/.output/public ./public/

VOLUME /app/videos
VOLUME /app/public
VOLUME /app/database

ENV Host=:3000
ENV FolderVideoQualitysPriv=./videos/qualitys
ENV FolderVideoQualitysPub=/videos/qualitys
ENV FolderVideoUploadsPriv=./videos/uploads
ENV StatsDriveName=nvme0n1

EXPOSE 3000

CMD ["./main.bin", "serve:main"]
