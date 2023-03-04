docker build . --platform linux/amd64 -f Dockerfile.demo -t kirari04/videocms:demo --push
docker build . --platform linux/arm64 -f Dockerfile.arm64.demo -t kirari04/videocms:demo_arm64 --push