FROM alpine:latest AS frontend

WORKDIR /app

RUN apk add --no-cache git

# Clone the frontend repository
RUN git clone https://github.com/Kirari04/videocms-frontend.git .

FROM oven/bun:latest AS frontend_build

WORKDIR /app

COPY --from=frontend /app .

RUN echo "NUXT_PUBLIC_API_URL=/api" >> .env
RUN echo "NUXT_PUBLIC_BASE_URL=" >> .env
RUN echo "NUXT_PUBLIC_DOCKER_HUB_TAG=kirari04/videocms:beta" >> .env
RUN echo "NUXT_PUBLIC_NAME=VideoCMS" >> .env
RUN echo "NUXT_PUBLIC_DEMO=false" >> .env

RUN bun install
RUN bun run generate

FROM golang:latest AS builder

WORKDIR /build

# Install Syft for SBOM generation
RUN go install github.com/anchore/syft/cmd/syft@latest

# Copy source code
COPY . .

# Tidy dependencies
RUN go mod tidy

# Build the Go binary
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags "-linkmode external -extldflags -static" -a -installsuffix cgo -o main_linux_amd64.bin main.go

# Generate SBOM for the application
# This scans the built binary and vendor dependencies
RUN /go/bin/syft packages . -o spdx-json=sbom.spdx.json

# Generate a checksum for the binary
RUN sha256sum main_linux_amd64.bin > main_linux_amd64.bin.sha256sum


FROM alpine:latest

WORKDIR /app

# System dependencies
RUN apk add --no-cache ffmpeg bash

# Copy the application binary from the builder stage
COPY --from=builder /build/main_linux_amd64.bin ./main.bin

# Copy the SBOM from the builder stage
COPY --from=builder /build/sbom.spdx.json /app/sbom.spdx.json

# Copy other necessary application files
COPY ./views ./views/
COPY ./public ./public/
COPY --from=frontend_build /app/.output/public ./public/

# Set up volumes for persistent data
VOLUME /app/videos
VOLUME /app/public
VOLUME /app/database

# Environment variables
ENV Host=:3000
ENV FolderVideoQualitysPriv=./videos/qualitys
ENV FolderVideoQualitysPub=/videos/qualitys
ENV FolderVideoUploadsPriv=./videos/uploads
ENV StatsDriveName=nvme0n1

# Expose the application port
EXPOSE 3000

# Define the command to run the application
CMD ["./main.bin", "serve:main"]