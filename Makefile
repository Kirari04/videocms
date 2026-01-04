dev:
	Host=:3002 air serve:main

publish:
	docker build --platform linux/amd64 -t kirari04/videocms:alpha --push .