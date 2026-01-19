# Video-CMS 🎬

[![Go Version](https://img.shields.io/badge/go-1.25-blue.svg)](https://golang.org/)
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
<img width="859" height="489" alt="{1CBD78E3-477B-4271-AF77-39A4F1C3C0E3}" src="https://github.com/user-attachments/assets/7eefd998-3adf-42a8-82f9-ee649d2811b0" />

### File Information
<img width="859" height="490" alt="{89B50AF3-8BFD-4EDA-8471-8833D4B73189}" src="https://github.com/user-attachments/assets/31ec8a32-792a-49fb-a6de-d2b4618c1b6d" />
<img width="856" height="220" alt="{A2167759-2941-43F3-9AFD-8F22D6253DAD}" src="https://github.com/user-attachments/assets/81381729-f1da-4892-bda1-2b929c1cbfc9" />


### Easy Export
<img width="859" height="490" alt="{FBB41552-30A2-4C70-AFAF-23DAC35EF6B5}" src="https://github.com/user-attachments/assets/8b810c16-0a95-4ea3-8f7e-15b087371627" />

### Quality Settings
<img width="875" height="848" alt="{F2015B0B-759F-4666-8B4B-A2217F819197}" src="https://github.com/user-attachments/assets/ba6edbd0-79e2-43a2-ac03-c7d4c75e7906" />


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
