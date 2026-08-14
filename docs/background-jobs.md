# Background jobs

VideoCMS runs all finite asynchronous work through one durable, embedded job runtime. Jobs, tasks, attempts, progress, events, queue state, and schedule state are stored in the application SQLite database. Restarting the process therefore does not lose queued work or its diagnostic history.

## Operations model

- A **job** is the user-visible operation, such as importing a video, processing media, downloading a remote file, preparing a viewer download, or deleting content.
- A **task** is an independently scheduled unit within a job. Import and remote-download tasks can add thumbnail and encoding children after the source file exists.
- An **attempt** records one execution of a task, including its worker, classification, bounded diagnostic text, and timestamps.
- An **event** records lifecycle and operator actions. User-facing job responses omit attempt diagnostics; administrators can inspect the redacted diagnostics in the task center.

The embedded runtime uses five capacity-controlled queues:

| Queue | Typical work | Default capacity |
| --- | --- | --- |
| `ffmpeg` | Encoding, thumbnails, prepared downloads | `MaxParallelFFmpegTasks` |
| `network` | Remote downloads | `MaxParallelDownloads` |
| `storage` | Imports and deletions | 2 |
| `maintenance` | Reconciliation and retention | 1 |
| `audit` | API-key audit writes | 1 |

FFmpeg work shares one global capacity limit. Higher task priority is combined with wait-time aging, so prepared downloads can start promptly without starving older media encodes.

## Reliability behavior

Tasks are claimed with a conditional database update. A transient failure is attempted up to four times total, normally after 5 seconds, 30 seconds, and 2 minutes. A handler can supply a shorter retry-after value for a known condition. Permanent failures stop immediately. Manual retry grants a fresh four-attempt budget while preserving the old attempts.

On process startup, running attempts become `interrupted` and their tasks return to the queue. Tasks already awaiting cancellation become canceled. Imports use stable creation keys and staging paths, making retries safe across both filesystem moves and database commits.

Cancellation propagates through Go contexts to downloads, FFmpeg, storage operations, and import probes. Disabling encoding, remote downloads, or prepared downloads cancels related queued and running jobs. Encoding reconciliation recreates canceled, unfinished artifacts once the feature is enabled again.

## Task center

Administrators can open **Background jobs** in the account panel to:

- filter and inspect jobs, tasks, attempts, progress, and timelines;
- cancel or retry a job or individual task;
- pause or resume queues without interrupting active work;
- run maintenance schedules immediately;
- inspect queue capacity and supervised continuous-service health.

Users see their own work under **Jobs** and can cancel or retry eligible jobs there. Polling is adaptive: active views refresh frequently, idle views slow down, and hidden tabs stop polling.

## HTTP API

Async submission endpoints respond with `202 Accepted`, a `Location` header pointing to the job, and `Retry-After: 2` where the response is the generic job envelope. The principal endpoints are:

```text
GET  /api/v2/jobs
GET  /api/v2/jobs/:id
POST /api/v2/jobs/:id/cancel
POST /api/v2/jobs/:id/retry

POST   /api/v2/uploads/simple
POST   /api/v2/uploads/:upload_id/finalize
POST   /api/v2/remote-downloads
DELETE /api/v2/file
DELETE /api/v2/files
DELETE /api/v2/folder
DELETE /api/v2/folders
```

Admin-only operations live below `/api/v2/admin` and include job/task controls, `/task-queues`, `/task-schedules`, and `/task-runtime`.

Clients should send an `Idempotency-Key` for simple uploads, remote-download batches, and deletions. Replaying the same key returns or reconnects to the same durable work; reusing it with a different upload or remote-download request returns `409 Conflict`.

## Maintenance and retention

Durable schedules replace ad-hoc ticker goroutines for source cleanup and encode reconciliation, deletion reconciliation, prepared-download expiry, upload expiry, API-audit cleanup, resource retention, and background-history retention. Schedule state and recent outcomes are visible in the task center.

Terminal job history is retained for 30 days. Successful API-audit jobs are retained for 24 hours because the actual audit entries have their own retention policy. Diagnostics are scrubbed for URL credentials, query strings, and common secret fields, then limited to 8 KiB.

## Upgrade cutover

The `unified-background-work-v1` migration runs once. It imports active legacy encodes, remote downloads, prepared downloads, and uploads plus terminal records updated within the previous 30 days. Work that was active at shutdown receives an interrupted attempt and is safely re-queued. Stable migration idempotency keys and a completion marker prevent duplicate backfills.
