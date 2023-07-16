echo RUNNING DOCKER BUILD AMD64
docker build . --platform linux/amd64 -f Dockerfile -t kirari04/videocms:alpha-1 --push
echo RUNNING DOCKER BUILD ARM64
docker build . --platform linux/arm64 -f Dockerfile.arm64 -t kirari04/videocms:alpha-1_arm64 --push

echo "===================================="
echo "================DONE================"
echo "===================================="