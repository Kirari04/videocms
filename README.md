# Video-CMS 🎬

[![Go Version](https://img.shields.io/badge/go-1.24-blue.svg)](https://golang.org/)
[![License](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
[![Docker Build](https://img.shields.io/badge/docker-build-brightgreen.svg)](https://hub.docker.com/r/kirari04/videocms)

A self-hosted Content Management System for your videos. 🎞️

## Features ✨

- **🏠 Self-host:** Host VideoCMS using Docker on your own hardware.
- **✍️ Pretty Subtitles:** Subtitles are stored as softsubs in the ASS format to preserve styling and save storage.
- **⚡ HLS Multi-Quality:** Videos are converted into multiple qualities to ensure smooth playback for different connection speeds.
- **🔊 Multi-Audio:** The player supports multiple audio tracks that are not stored inside the video, saving storage space.
- **🚀 Fast Chunked Upload:** Allows the server to be behind a proxy without requiring high maximum post limits.
- **📦 Dynamic MKV Download:** The server dynamically assembles subtitles, audio tracks, and video tracks during download without re-encoding.

## Documentation 📚

Follow the documentation to setup VideoCMS: [https://videocms-docs.vercel.app/](https://videocms-docs.vercel.app/)

## Screenshots 📸

### Simple Panel
![Alt text](./docs/image.png)

### Advanced File Information
![Alt text](./docs/image2.png)
![Alt text](./docs/image5.png)

### Easy Export
![Alt text](./docs/image3.png)
![Alt text](./docs/image4.png)

### Multiple Qualities
![Alt text](./docs/image6.png)

### Multiple Subtitles
![Alt text](./docs/image7.png)

### Multiple Audio Channels
![Alt text](./docs/image8.png)

### Embed in Chats (like Discord)
![Alt text](./docs/image9.png)

## Build 🛠️

```bash
docker build --platform linux/amd64 -t kirari04/videocms:alpha --push .
```