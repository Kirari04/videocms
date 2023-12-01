#!/bin/bash
bash scripts/\$default.sh

# Prompt the user for confirmation
read -p "Do you want to run the Binary Build Script? (yes/no): " answerbin
if [ "$answerbin" = "yes" ]; then
    rm -rf ./build/cmd
    # BINARIES
    mkdir -p ./build/cmd
    # MAIN
    echo RUNNING GO BUILD MAIN
    ## amd64
    # echo RUNNING GO BUILD windows amd64
    # CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-linkmode external -extldflags -static" -a -installsuffix cgo -o build/cmd/main_windows_amd64.exe main.go
    # sha256sum build/cmd/main_windows_amd64.exe > build/cmd/main_windows_amd64.exe.sha256sum
    # gpg --detach-sig --armor build/cmd/main_windows_amd64.exe
    echo RUNNING GO BUILD linux amd64
    CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags "-linkmode external -extldflags -static" -a -installsuffix cgo -o build/cmd/main_linux_amd64.bin main.go
    sha256sum build/cmd/main_linux_amd64.bin > build/cmd/main_linux_amd64.bin.sha256sum
    # gpg --detach-sig --armor build/cmd/main_linux_amd64.bin
    ## arm64
    echo RUNNING GO BUILD linux arm64
    CGO_ENABLED=1 CC=aarch64-linux-gnu-gcc-11 CC_FOR_TARGET=gcc-11-aarch64-linux-gnu GOOS=linux GOARCH=arm64 go build -ldflags "-linkmode external -extldflags -static" -a -installsuffix cgo -o build/cmd/main_linux_arm64.bin main.go
    sha256sum build/cmd/main_linux_arm64.bin > build/cmd/main_linux_arm64.bin.sha256sum
    # gpg --detach-sig --armor build/cmd/main_linux_arm64.bin

    # Console
    echo RUNNING GO BUILD CONSOLE
    ## amd64
    # echo RUNNING GO BUILD windows amd64
    # CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-linkmode external -extldflags -static" -a -installsuffix cgo -o build/cmd/console_windows_amd64.exe console/console.go
    # sha256sum build/cmd/console_windows_amd64.exe > build/cmd/console_windows_amd64.exe.sha256sum
    # gpg --detach-sig --armor build/cmd/console_windows_amd64.exe
    echo RUNNING GO BUILD linux amd64
    CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags "-linkmode external -extldflags -static" -a -installsuffix cgo -o build/cmd/console_linux_amd64.bin console/console.go
    sha256sum build/cmd/console_linux_amd64.bin > build/cmd/console_linux_amd64.bin.sha256sum
    # gpg --detach-sig --armor build/cmd/console_linux_amd64.bin
    ## arm64
    echo RUNNING GO BUILD linux arm64
    CGO_ENABLED=1 CC=aarch64-linux-gnu-gcc-11 CC_FOR_TARGET=gcc-11-aarch64-linux-gnu GOOS=linux GOARCH=arm64 go build -ldflags "-linkmode external -extldflags -static" -a -installsuffix cgo -o build/cmd/console_linux_arm64.bin console/console.go
    sha256sum build/cmd/console_linux_arm64.bin > build/cmd/console_linux_arm64.bin.sha256sum
    # gpg --detach-sig --armor build/cmd/console_linux_arm64.bin
fi

# Prompt the user for confirmation
read -p "Do you want to run the Panel Build Script? (yes/no): " answer

# Proceed with the script only if the answer is "yes"
if [ "$answer" = "yes" ]; then
    rm -fr ./build/svelte
    # PANEL
    git clone git@github.com:Kirari04/videocms-frontend.git ./build/svelte
    cd ./build/svelte
    bun install
    cat > ./.env <<- EOF
NUXT_PUBLIC_API_URL=http://localhost/api
NUXT_PUBLIC_BASE_URL=http://localhost
NUXT_PUBLIC_DOCKER_HUB_TAG=localhost
NUXT_PUBLIC_API_DOCS=http://localhost
NUXT_PUBLIC_TUTORIAL=http://localhost
EOF
    
    bun run generate
    mkdir exportdata
    mv -r ./dist ./exportdata
    cd ../../
fi

# DOCKER
export DOCKER_BUILDKIT=1
# echo RUNNING DOCKER BUILD AMD64
# docker build . --platform linux/amd64 -f Dockerfile -t kirari04/videocms:alpha-1 --push
# echo RUNNING DOCKER BUILD ARM64
# docker build . --platform linux/arm64 -f Dockerfile.arm64 -t kirari04/videocms:alpha-1_arm64 --push

echo RUNNING DOCKER BUILD AMD64 PANEL
docker build . --platform linux/amd64 -f Dockerfile.panel -t kirari04/videocms:panel --push
# echo RUNNING DOCKER BUILD ARM64 PANEL
# docker build . --platform linux/arm64 -f Dockerfile.panel.arm64 -t kirari04/videocms:panel_arm64 --push

echo "DONE"