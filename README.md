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
make docker-build
```

## Docker Images

Images are published to both registries:

- `ghcr.io/kirari04/videocms`
- `kirari04/videocms`

Available tags:

- `staging`: release-candidate image built from the `staging` branch.
- `beta`: accepted beta/default release built from `master`.
- `latest`: same image as `beta` while VideoCMS is still in beta.
- `vX.Y.Z`: immutable version tag for an accepted `master` release.

## Release Flow

Development work starts on feature branches and opens pull requests into `dev`.
The `dev` branch is the active integration branch and only runs validation:
Go tests, vet/build checks, frontend generation, docs build, and Docker build
validation. It does not publish Docker images.

When `dev` is ready for release testing, open a pull request from `dev` into
`staging`. A push to `staging` publishes only the mutable `staging` Docker tag
to GHCR and Docker Hub.

When staging is accepted, open a pull request from `staging` into `master`.
The `master` branch is release-only. Every accepted push to `master` verifies
the project, resolves the next patch version, builds Linux binaries, publishes
multi-arch Docker images, creates the Git tag, creates or updates the GitHub
Release, and uploads checksums.

After the workflow changes are merged to `master`, repository maintainers can
create the `dev` and `staging` branches, set `dev` as the default branch, and
apply branch/tag protections with:

```bash
scripts/setup-release-flow.sh
```
