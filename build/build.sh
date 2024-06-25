#!/bin/bash

# DOCKER
export DOCKER_BUILDKIT=1

# Prompt the user for confirmation
read -p "Do you want to run the Build for linux/amd64? (yes/no): " answerbin
read -p "Do you want to run the Build for linux/arm64? (yes/no): " answerbinarm
if [ "$answerbin" = "yes" ]; then
    # echo RUNNING DOCKER BUILD AMD64
    docker build . --platform linux/amd64 -f Dockerfile -t kirari04/videocms:alpha --push
    # gpg --detach-sig --armor build/cmd/main_linux_amd64.bin
fi

if [ "$answerbinarm" = "yes" ]; then
    ## arm64
    # echo RUNNING DOCKER BUILD ARM64
    docker build . --platform linux/arm64 -f Dockerfile.arm64 -t kirari04/videocms:alpha_arm64 --push
    # gpg --detach-sig --armor build/cmd/main_linux_arm64.bin
fi

echo "DONE"