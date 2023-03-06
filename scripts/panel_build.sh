echo RUNNING DOCKER BUILD AMD64 PANEL
docker build . --platform linux/amd64 -f Dockerfile.panel -t kirari04/videocms:panel --push --no-cache
echo RUNNING DOCKER BUILD ARM64 PANEL
docker build . --platform linux/arm64 -f Dockerfile.panel.arm64 -t kirari04/videocms:panel_arm64 --push --no-cache

echo "===================================="
echo "================DONE================"
echo "===================================="