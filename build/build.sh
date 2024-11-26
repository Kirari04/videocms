#!/bin/bash

# DOCKER
export DOCKER_BUILDKIT=1
docker build . --platform linux/amd64 -f Dockerfile -t kirari04/videocms:alpha --push