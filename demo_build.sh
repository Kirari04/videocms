echo RUNNING DOCKER BUILD AMD64 DEMO
docker build . --platform linux/amd64 -f Dockerfile.demo -t kirari04/videocms:demo --push
echo RUNNING DOCKER BUILD ARM64 DEMO
docker build . --platform linux/arm64 -f Dockerfile.arm64.demo -t kirari04/videocms:demo_arm64 --push