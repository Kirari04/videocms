# Video-CMS 🎬

[![CI](https://github.com/Kirari04/videocms/actions/workflows/ci.yml/badge.svg?branch=dev)](https://github.com/Kirari04/videocms/actions/workflows/ci.yml?query=branch%3Adev)
[![Staging Image](https://github.com/Kirari04/videocms/actions/workflows/staging-image.yml/badge.svg?branch=staging)](https://github.com/Kirari04/videocms/actions/workflows/staging-image.yml?query=branch%3Astaging)
[![Release](https://github.com/Kirari04/videocms/actions/workflows/release.yml/badge.svg?branch=master)](https://github.com/Kirari04/videocms/actions/workflows/release.yml?query=branch%3Amaster)
[![Go Version](https://img.shields.io/github/go-mod/go-version/Kirari04/videocms?filename=go.mod&logo=go)](https://go.dev/)
[![Docker Pulls](https://img.shields.io/docker/pulls/kirari04/videocms?logo=docker)](https://hub.docker.com/r/kirari04/videocms)
[![License](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)

A self-hosted Content Management System for your videos. 🎞️

## Features ✨

- **🔗 Public Video Links:** Videos are public by default through unlisted UUID links; anyone with a link can watch without an account.
- **🏠 Self-host:** Host VideoCMS using Docker on your own hardware.
- **✍️ Pretty Subtitles:** Subtitles are stored as softsubs in the ASS format to preserve styling and save storage.
- **⚡ HLS Multi-Quality:** Videos are converted into multiple qualities to ensure smooth playback for different connection speeds.
- **🔊 Multi-Audio:** The player supports multiple audio tracks that are not stored inside the video, saving storage space.
- **🚀 Fast Chunked Upload:** Allows the server to be behind a proxy without requiring high maximum post limits.
- **📦 Configurable Downloads:** Viewers can choose a ready quality and download MP4 with one audio track, or MKV with selectable audio and subtitle tracks. The server assembles the result without re-encoding.
- **🗄️ Storage Pools:** Keep local storage as the default or route new uploads across S3-compatible buckets and SFTP folders from the admin UI.
- **🔄 Safe Storage Migrations:** Move all videos or one account between pools with resumable copies, verification, atomic cutover, and delayed source cleanup.
- **💾 Read-Through Caches:** Cache frequently played remote media on inexpensive storage and track cache effectiveness in System Stats.
- **🧭 Durable Background Jobs:** Monitor uploads, downloads, encoding, migrations, and maintenance with persistent progress, retries, pause, and cancellation controls.
- **🔗 Folder Sharing:** Share a folder and its subfolders without visitor accounts. Give each link view/download, upload-only, or combined access, with optional passwords, CAPTCHA, and expiration.

## Documentation 📚

Follow the documentation to setup VideoCMS: [https://videocms-docs.vercel.app/](https://videocms-docs.vercel.app/)

Storage mounts, pool routing, upgrade behavior, and reconnecting migrated storage are described in [the storage guide](https://videocms-docs.vercel.app/operations/storage).

Monitoring, canceling, retrying, and troubleshooting background work are described in [the background-jobs guide](https://videocms-docs.vercel.app/operations/background-jobs).

### Folder share links

Choose **Share folder** from a folder's action menu or while browsing inside it. Create multiple independent links, change their permissions, or revoke them from the same dialog. View access includes downloads when server downloads are enabled. Upload-only visitors can send videos without listing or playing existing content; their uploads use the folder owner's storage quota and processing pipeline.

Password and CAPTCHA checks unlock a share for up to six hours. Changes to a share invalidate its visitor sessions and media access at the next request. Shares stop working when they expire or their root folder or owner is deleted. Moving a subfolder outside the shared tree removes it from the share.

Each upload-enabled share can set a maximum video size and a total folder size limit. The folder limit counts original video sizes across the shared folder and its subfolders, plus full-size reservations for pending uploads. Uploads which would exceed it are blocked before transferring; cancellation or expiry releases the reservation. The server’s maximum video size and the owner’s storage quota still apply. Blank limits use the server maximum for videos and no additional folder limit.

Visitors receive their uploaded video’s public link after processing by default. Disable **Give visitors public links to their uploads** in the link editor to return only an upload receipt. Public video links work independently of the folder share’s password, CAPTCHA, and expiration.

Guest uploads target the folder currently open in the visitor’s browser. They recheck access on every chunk and before accepting the completed upload for ingestion. Revoking a share stops unfinished transfers; it does not cancel ingestion of uploads already accepted.

CAPTCHA requires an enabled, configured server provider. Guest uploads use the same resumable tus uploader as account uploads, including bounded chunks, retries, and adaptive parallel transfers for large files. Each request rechecks the share's current permissions and destination; incomplete uploads expire through the standard cleanup. Individual video links remain independently accessible; protecting a folder share does not make existing video links private.

The default deployment serves the share page, API and player from the same origin. A separate frontend on the same site can use `NUXT_PUBLIC_API_URL` and `NUXT_PUBLIC_BASE_URL` with the backend's credentialed CORS settings and an explicit frontend origin; its Content Security Policy must also allow the API origin in `connect-src`. Protected shares use `SameSite=Lax` cookies; frontends hosted on a different site should proxy the backend through their own origin.

Owner APIs are `GET/POST /api/folder-shares` and `PUT/DELETE /api/folder-shares/:id`. Public visitors use `/s/:token`, backed by `GET /api/shares/:token` and `POST /api/shares/:token/unlock`. Guest tus requests use `/api/uploads` with an `X-Folder-Share: :token` header, the visitor's unlock cookie when required, and `filename`, `parent_folder_id`, `client_upload_uuid`, and `file_size` (full video bytes) metadata. After transfer, `POST /api/shares/:token/uploads/:upload_id/finalize` queues ingestion and returns a receipt without owner job or media URLs. When `ReturnUploadLink` is enabled, `GET /api/shares/:token/uploads/:upload_id` returns processing status and, when ready, only that upload’s public video URL. Owner creation and updates accept `FolderID`, `Name`, `AllowRead`, `AllowUpload`, `RequireCaptcha`, optional `ExpiresAt`, optional `Password`, and optional `MaxVideoSize`, `MaxFolderSize` (bytes, zero means default), and `ReturnUploadLink` (defaults to true). Omitted upload settings are preserved on update. Omit `Password` to preserve it; use an empty string to remove it.

## Screenshots 📸

### Landing
![VideoCMS landing page](./docs/redesign-screenshots/landing-desktop.png)

### Dashboard
![VideoCMS dashboard](./docs/redesign-screenshots/dashboard-desktop.png)

### Library
![VideoCMS video library](./docs/redesign-screenshots/library-desktop.png)

### Upload Manager
![VideoCMS upload manager](./docs/redesign-screenshots/upload-desktop.png)

### Encoding Queue
![VideoCMS encoding queue](./docs/redesign-screenshots/encodings-desktop.png)

### System Stats
![VideoCMS system stats](./docs/redesign-screenshots/system-stats-desktop.png)

### Mobile Library
![VideoCMS mobile library](./docs/redesign-screenshots/library-mobile.png)

## Build 🛠️

```bash
make docker-build
```

## Development

Initialize the frontend and documentation submodules, then start the complete development stack:

```bash
git submodule update --init --recursive
make dev
```

Nuxt is available at `http://127.0.0.1:3000` and proxies `/api` and the backend-owned media routes to the hot-reloading Go server on port `3001`. Stopping either process shuts down the other one cleanly.

## Docker Images

Published images are available from:

- `ghcr.io/kirari04/videocms`
- `kirari04/videocms`

Available tags:

- `latest`
- `beta`
- `staging`
- `vX.Y.Z`
