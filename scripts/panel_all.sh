export DOCKER_BUILDKIT=1
echo RUNNING DOCKER BUILD panel_build
bash ./scripts/panel_build.sh &> ./scripts/log/panel_build.log &
sleep 1
echo RUNNING DOCKER BUILD demo_panel_build
bash ./scripts/demo_panel_build.sh &> ./scripts/log/demo_panel_build.log &