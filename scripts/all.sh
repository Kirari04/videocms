export DOCKER_BUILDKIT=1
echo RUNNING DOCKER BUILD
bash ./scripts/build.sh &> ./scripts/log/build.log &
sleep 1
bash ./scripts/panel_build.sh &> ./scripts/log/panel_build.log &
sleep 1
bash ./scripts/demo_build.sh &> ./scripts/log/demo_build.log &
sleep 1
bash ./scripts/demo_panel_build.sh &> ./scripts/log/demo_panel_build.log &