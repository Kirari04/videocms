dev:
	Host=:3000 air serve:main

publish:
	docker build --platform linux/amd64 -t kirari04/videocms:alpha --push .