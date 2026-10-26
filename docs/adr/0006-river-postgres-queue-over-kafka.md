# ADR 0006: River (Postgres-backed queue) for the pipeline; Kafka deferred

**Status:** accepted · **Date:** 2026-10-26

## Context
Each upload runs a multi-stage pipeline (parse → normalize → match → classify → report) that must survive worker crashes, never double-count, and keep a per-user ordering. The spec lists Kafka as a Phase 5 option behind a transactional outbox.

## Decision
Use River (`riverqueue/river`) with the pgx driver. Every stage writes its results **and** enqueues the next job inside the same Postgres transaction, so a job either fully happened or did not. Retries: 3 attempts with River's exponential backoff; the final failure marks the user-visible job `FAILED` with a readable message.

## Rationale
- Transactional enqueue is the whole reliability story. With Kafka the same guarantee needs an outbox table plus a relay plus idempotent consumers: three moving parts for a single-node app.
- One datastore to run locally (Postgres is already required). River's own tables are migrated at startup.
- Throughput is nowhere near a queue's limits: a 100k-trade recompute is one job, not 100k messages.

## Consequences
- Recompute jobs are deliberately **not** unique: River's unique-job option must include the `running` state, which would drop a request that arrives while a recompute is in flight (and could not have seen the new trades). Duplicates are cheap, idempotent and serialised by the per-user advisory lock.
- Progress reaches the browser through `job_events` rows (SSE polls the table), not a message bus, so a reconnect with `Last-Event-ID` replays nothing twice.
- Kafka stays a Phase 5 experiment (outbox relay) and will need its own ADR if it ever proves worth the extra operational surface.
