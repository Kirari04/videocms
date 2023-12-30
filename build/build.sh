#!/bin/bash
bash scripts/\$default.sh

# Prompt the user for confirmation
read -p "Do you want to run the Binary Build Script? (yes/no): " answerbin
if [ "$answerbin" = "yes" ]; then
    rm -rf ./build/cmd
    mkdir -p ./build/cmd
    echo RUNNING GO BUILD MAIN
    
    ## amd64
    echo RUNNING GO BUILD linux amd64
    CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags "-linkmode external -extldflags -static" -a -installsuffix cgo -o build/cmd/main_linux_amd64.bin main.go
    sha256sum build/cmd/main_linux_amd64.bin > build/cmd/main_linux_amd64.bin.sha256sum
    # gpg --detach-sig --armor build/cmd/main_linux_amd64.bin

    ## arm64
    echo RUNNING GO BUILD linux arm64
    CGO_ENABLED=1 CC=aarch64-linux-gnu-gcc-11 CC_FOR_TARGET=gcc-11-aarch64-linux-gnu GOOS=linux GOARCH=arm64 go build -ldflags "-linkmode external -extldflags -static" -a -installsuffix cgo -o build/cmd/main_linux_arm64.bin main.go
    sha256sum build/cmd/main_linux_arm64.bin > build/cmd/main_linux_arm64.bin.sha256sum
    # gpg --detach-sig --armor build/cmd/main_linux_arm64.bin
fi

# DOCKER
export DOCKER_BUILDKIT=1
# echo RUNNING DOCKER BUILD AMD64
docker build . --platform linux/amd64 -f Dockerfile -t kirari04/videocms:alpha --push
# echo RUNNING DOCKER BUILD ARM64
docker build . --platform linux/arm64 -f Dockerfile.arm64 -t kirari04/videocms:alpha_arm64 --push

echo "DONE"