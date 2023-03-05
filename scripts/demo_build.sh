echo RUNNING DOCKER BUILD AMD64 DEMO
docker build . --platform linux/amd64 -f Dockerfile.demo -t kirari04/videocms:demo --push
echo RUNNING DOCKER BUILD ARM64 DEMO
docker build . --platform linux/arm64 -f Dockerfile.demo.arm64 -t kirari04/videocms:demo_arm64 --push
echo RUNNING DOCKER BUILD AMD64 DEMO PANEL
docker build . --platform linux/amd64 -f Dockerfile.panel.demo -t kirari04/videocms:demo_panel --push
echo RUNNING DOCKER BUILD ARM64 DEMO PANEL
docker build . --platform linux/arm64 -f Dockerfile.panel.demo.arm64 -t kirari04/videocms:demo_panel_arm64 --push