# ADR 0005: The engine is pure; all I/O lives at the edges

**Status:** accepted · **Date:** 2026-10-26

## Context
Deterministic replay (same inputs + same rules version → byte-identical report) and fast, exhaustive testing both need the calculation to be free of hidden state.

## Decision
`parser`, `normalize`, `corpaction`, `matcher`, `tax` and `pipeline.Run` take values and return values. No clock, no database, no randomness, no map-iteration-order leaks: every output slice is sorted by content-derived keys and IDs are SHA-256 digests of the inputs that define them. The CLI, the golden tests and (from Phase 2) the background worker call the same `pipeline.Run`.

## Consequences
- Every golden case runs the pipeline twice and asserts identical checksums.
- `TestPropertyInputOrderIndependence` shuffles the input and expects identical results.
- The database stores derived tables (lots, matches, gains) that are always rebuilt from trades plus reference data, never edited by hand.
