export DOCKER_BUILDKIT=1
echo RUNNING DOCKER BUILD
bash build.sh
bash demo_build.sh
bash panel_build.sh