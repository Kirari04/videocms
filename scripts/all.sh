export DOCKER_BUILDKIT=1
echo RUNNING DOCKER BUILD
bash ./scripts/build.sh
bash ./scripts/panel_build.sh
bash ./scripts/demo_build.sh
bash ./scripts/demo_panel_build.sh