.PHONY: dev docker-build dckb dcktest publish bump-minor bump-patch

VERSION ?= $(shell tr -d '\n' < VERSION.txt)
LOCAL_IMAGE ?= videocms:local

dev:
	Host=:3000 go tool air serve:main

docker-build:
	docker buildx build \
		-f Dockerfile \
		--platform linux/amd64 \
		--build-arg VERSION=$(VERSION) \
		--build-arg CHANNEL=local \
		--build-arg DOCKER_IMAGE_TAG=$(LOCAL_IMAGE) \
		-t $(LOCAL_IMAGE) \
		--load \
		.

dckb: docker-build

dcktest:
	docker run --rm -it -p 3000:3000 $(LOCAL_IMAGE)

publish:
	@echo "Publishing is handled by GitHub Actions from staging and master."
	@exit 1

bump-minor:
	@NEW_VERSION=$$(awk -F. '{print $$1"."$$2+1".0"}' VERSION.txt); \
	echo "Bumping minor to $$NEW_VERSION"; \
	printf "%s" "$$NEW_VERSION" > VERSION.txt; \
	sed -i "s/var VERSION string = \".*\"/var VERSION string = \"$$NEW_VERSION\"/" config/config.go

bump-patch:
	@NEW_VERSION=$$(awk -F. '{print $$1"."$$2"."$$3+1}' VERSION.txt); \
	echo "Bumping patch to $$NEW_VERSION"; \
	printf "%s" "$$NEW_VERSION" > VERSION.txt; \
	sed -i "s/var VERSION string = \".*\"/var VERSION string = \"$$NEW_VERSION\"/" config/config.go
