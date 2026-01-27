dev:
	Host=:3000 air serve:main

publish:
	docker build -f Dockerfile --platform linux/amd64 -t kirari04/videocms:beta --push . --no-cache

dckb:
	docker build -f Dockerfile --platform linux/amd64 -t kirari04/videocms:beta --load .

dcktest:
	docker run --rm -it -p 3000:3000 kirari04/videocms:beta