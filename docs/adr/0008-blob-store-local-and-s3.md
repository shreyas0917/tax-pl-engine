# ADR 0008: BlobStore interface with local and S3 drivers; local by default

**Status:** accepted · **Date:** 2026-10-26

## Context
Raw tradebooks and generated reports are files, not rows. They must be deletable (24-hour purge of raw files, "delete my data"), and the app must run on a laptop with nothing but Docker, Go and Node.

## Decision
`internal/blob.Store` has four methods (Put, Get, Delete, Exists). `BLOB_DRIVER=local` (default) stores under `BLOB_LOCAL_DIR` with atomic temp-file-and-rename writes and path sanitisation; `BLOB_DRIVER=s3` uses the AWS SDK v2 in path-style mode against MinIO (`docker compose --profile s3 up`) or any S3.

## Rationale
- The spec requires full function without MinIO; local disk is the simplest thing that is correct.
- The interface keeps the purge and delete-my-data jobs driver-agnostic and testable with a temp directory.

## Consequences
- Blob keys encode the owner (`raw/<user>/<sha>.<ext>`, `reports/<user>/<id>.<format>`), so deletion by user is a prefix walk in either driver.
- Report downloads stream from the store; the checksum recorded at generation time is sent as a header so clients can verify.
