dev:
	Host=:3000 air serve:main

publish:
	docker build -f Dockerfile --platform linux/amd64 -t kirari04/videocms:beta --push . --no-cache

dckb:
	docker build -f Dockerfile --platform linux/amd64 -t kirari04/videocms:beta --load .

dcktest:
	docker run --rm -it -p 3000:3000 kirari04/videocms:beta

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
