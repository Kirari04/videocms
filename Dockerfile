# syntax=docker/dockerfile:1.7

ARG ALPINE_VERSION=3.22
ARG BUN_VERSION=1.2.18
ARG GO_VERSION=1.26.0

FROM --platform=$BUILDPLATFORM oven/bun:${BUN_VERSION} AS frontend_build

WORKDIR /app

ARG CHANNEL=beta
ARG DOCKER_IMAGE_TAG=kirari04/videocms:beta

COPY videocms-frontend/package.json videocms-frontend/bun.lock ./
RUN --mount=type=cache,target=/root/.bun/install/cache \
    bun install --frozen-lockfile

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

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=v0.0.0-dev
ARG CHANNEL=dev

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Keep frontend, documentation, and repository metadata out of the Go build
# cache key. Add new top-level Go package directories here when introduced.
COPY *.go ./
COPY app ./app
COPY auth ./auth
COPY background ./background
COPY cmd ./cmd
COPY config ./config
COPY configdb ./configdb
COPY controllers ./controllers
COPY download ./download
COPY helpers ./helpers
COPY inits ./inits
COPY logic ./logic
COPY mediacache ./mediacache
COPY middlewares ./middlewares
COPY models ./models
COPY routes ./routes
COPY services ./services
COPY storage ./storage
COPY traffic ./traffic

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /out && \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build \
    -ldflags "-linkmode external -extldflags -static -X ch/kirari04/videocms/config.VERSION=${VERSION}" \
    -o /out/videocms \
    ./main.go

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

COPY ./views ./views/
COPY ./public ./public/
COPY --from=frontend_build /app/.output/public ./public/

VOLUME /app/videos
VOLUME /app/database

ENV Host=:3000
ENV FolderVideoQualitysPriv=./videos/qualitys
ENV FolderVideoQualitysPub=/videos/qualitys
ENV FolderVideoUploadsPriv=./videos/uploads
ENV StorageScratchDir=./videos/scratch
ENV StatsDriveName=nvme0n1

EXPOSE 3000

CMD ["./main.bin", "serve:main"]
