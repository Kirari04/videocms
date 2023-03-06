echo RUNNING DOCKER BUILD AMD64 DEMO PANEL
docker build . --platform linux/amd64 -f Dockerfile.demo.panel -t kirari04/videocms:demo_panel --push --no-cache
echo RUNNING DOCKER BUILD ARM64 DEMO PANEL
docker build . --platform linux/arm64 -f Dockerfile.demo.panel.arm64 -t kirari04/videocms:demo_panel_arm64 --push --no-cache

echo "===================================="
echo "================DONE================"
echo "===================================="