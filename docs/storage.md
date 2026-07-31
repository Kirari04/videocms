# Storage mounts and upload pools

VideoCMS always starts with its built-in local storage. Administrators can add any number of S3-compatible mounts in **Administration → Storage**, group mounts into upload pools, choose the instance default pool, and optionally assign a different pool to an individual user.

Each file lives on exactly one mount. VideoCMS does not replicate objects between mounts; backups and replication remain the responsibility of the storage infrastructure.

## Upgrading an existing installation

No manual media or database migration is required. On startup, VideoCMS automatically:

- creates the storage mount and pool tables;
- registers the existing local media directory as the built-in `local` mount;
- creates a built-in local upload pool and selects it as the default when no other default exists; and
- marks existing file records as available and assigns legacy records without a storage ID to `local`.

Existing files remain at their current paths. Adding a remote mount does not move them, and changing the default pool only affects new uploads.

## Configure credential encryption

Remote adapter credentials cannot be saved until `StorageEncryptionKey` is present in the server environment. Generate a 32-byte key:

```bash
openssl rand -base64 32
```

Pass the result to the VideoCMS container or process:

```yaml
services:
  videocms:
    environment:
      StorageEncryptionKey: "replace-with-the-generated-value"
```

Restart VideoCMS after adding the key. The key encrypts adapter credentials in the database using AES-256-GCM. Keep it with the installation's other secrets and backups: losing or changing it makes saved credentials unreadable. The key is not required for installations that only use local storage.

If the key is lost or intentionally changed, the objects remain intact. Set a valid new key, restart VideoCMS, edit each affected mount to enter its credentials again, then mount and reconnect it.

## Add an S3-compatible mount

Open **Administration → Storage**, select **Add S3 mount**, and enter:

- a display name;
- bucket and region;
- an optional endpoint and path-style mode for providers such as MinIO;
- an optional object prefix; and
- an access key ID and secret access key, or leave all credential fields empty to use the server's AWS credential provider chain.

The bucket must already exist. VideoCMS checks the connection before saving the mount. A typical storage policy needs permission to list the bucket and to get, put, and delete objects below the configured prefix, including multipart-upload operations.

While a mount is connected, its display name, credentials, and upload tuning can be changed. Detach it before changing the bucket, region, endpoint, prefix, or path-style mode so existing file records cannot silently point at another object namespace.

## Route new uploads

Create a pool with one or more mounts and make it the instance default, or choose the pool on an individual user's admin form. For every new file, VideoCMS orders the mounted members by the number of file bytes currently tracked on each mount and writes to the least-used member. Ties are deterministic. If that write fails, it tries the next available member.

Pool changes never move existing files. The selected mount ID is stored on the file record and is used for all later reads, encoding outputs, and deletion.

## Detach and reconnect a mount

Any additional mount can be detached even when it owns files. Detaching:

- removes the mount from new upload placement and runtime reads;
- marks its active files as unavailable;
- keeps the mount identity, encrypted configuration, pool membership, and file records; and
- does not delete or move any objects in the bucket.

Unavailable files remain visible in the library, but playback and export are disabled until their storage is reconnected.

To reconnect the same bucket, select **Mount**. To migrate the bucket, detach the mount, preserve the same per-file object paths below the configured prefix, edit its endpoint, bucket, prefix, or credentials, and mount it again. VideoCMS validates persisted source and completed output-manifest objects before relinking unavailable file IDs. The **Scan and reconnect files** action can also preview the number of matches before applying the relink.

Scans use bounded concurrency and apply results in batches, so retrying after an interruption safely resumes remaining work. Connecting a brand-new mount also performs this scan, so a replacement mount can recover records whose previous mount no longer exists. Relinking updates database records only; it never copies, modifies, or deletes objects.
