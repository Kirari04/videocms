docker build . --platform linux/amd64 -f Dockerfile -t kirari04/videocms:alpha-1 --push
docker build . --platform linux/arm64 -f Dockerfile.arm64 -t kirari04/videocms:alpha-1_arm64 --push