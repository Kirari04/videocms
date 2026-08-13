.PHONY: dev dev-backend dev-frontend docker-build dckb dcktest publish bump-minor bump-patch

VERSION ?= $(shell tr -d '\n' < VERSION.txt)
LOCAL_IMAGE ?= videocms:local
DEV_FRONTEND_HOST ?= 127.0.0.1
DEV_FRONTEND_PORT ?= 3000
DEV_BACKEND_HOST ?= 127.0.0.1
DEV_BACKEND_PORT ?= 3001
DEV_BACKEND_URL ?= http://$(DEV_BACKEND_HOST):$(DEV_BACKEND_PORT)
DEV_PUBLIC_ORIGIN ?= http://$(DEV_FRONTEND_HOST):$(DEV_FRONTEND_PORT)
DEV_MEDIA_PATH ?= /videos/qualitys
# Stable, development-only key so credentials saved in the local dev database
# remain readable across restarts. Production must supply its own secret.
DEV_STORAGE_ENCRYPTION_KEY ?= $(if $(StorageEncryptionKey),$(StorageEncryptionKey),MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=)

dev:
	@DEV_FRONTEND_HOST="$(DEV_FRONTEND_HOST)" \
	DEV_FRONTEND_PORT="$(DEV_FRONTEND_PORT)" \
	DEV_BACKEND_HOST="$(DEV_BACKEND_HOST)" \
	DEV_BACKEND_PORT="$(DEV_BACKEND_PORT)" \
	DEV_BACKEND_URL="$(DEV_BACKEND_URL)" \
	DEV_PUBLIC_ORIGIN="$(DEV_PUBLIC_ORIGIN)" \
	DEV_MEDIA_PATH="$(DEV_MEDIA_PATH)" \
	DEV_STORAGE_ENCRYPTION_KEY="$(DEV_STORAGE_ENCRYPTION_KEY)" \
	./scripts/dev.sh

dev-backend:
	Host="$(DEV_BACKEND_HOST):$(DEV_BACKEND_PORT)" \
	FolderVideoQualitysPub="$(DEV_MEDIA_PATH)" \
	StorageEncryptionKey="$(DEV_STORAGE_ENCRYPTION_KEY)" \
	go tool air serve:main

dev-frontend:
	cd videocms-frontend && \
	NUXT_PUBLIC_API_URL="/api" \
	NUXT_PUBLIC_BASE_URL="$(DEV_PUBLIC_ORIGIN)" \
	NUXT_DEV_BACKEND_URL="$(DEV_BACKEND_URL)" \
	NUXT_DEV_MEDIA_PATH="$(DEV_MEDIA_PATH)" \
	bun run dev --host "$(DEV_FRONTEND_HOST)" --port "$(DEV_FRONTEND_PORT)"

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
