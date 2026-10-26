# Benchmarks

Measured with `backend/cmd/bench` (`make bench`), which generates a realistic multi-year
history (random buys, sells capped at holdings, an 8% same-day round-trip rate) across
accounts and ISINs and times each stage of the **pure** pipeline in-process. No database
or network is involved, so these are the engine's own costs.

Machine: Windows 11, Go 1.26, single process, default GC. Numbers are from one run on
26 October 2026 while other workloads (a Vite dev server and Playwright) were active, so
treat them as upper bounds.

| Trades | Accounts | ISINs | Generate | Match (FIFO + netting) | Classify + summarise | Full pipeline | JSON report | Peak heap |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 100,000 | 3 | 200 | 1.10 s | 3.55 s | 2.01 s | 5.89 s | 11.75 s (157.1 MB) | 353.8 MB |
| 250,000 | 3 | 200 | 1.61 s | 11.33 s | 5.31 s | 26.53 s | 43.09 s (396.6 MB) | 725.2 MB |
| 500,000 | 3 | 200 | 4.43 s | 21.03 s | 12.61 s | 36.04 s | 98.52 s (790.9 MB) | 658.1 MB |
| 1,000,000 | 3 | 200 | — | — | — | out of memory on the test machine (see below) | — | — |

100,000 trades produced 50,756 lots, 73,841 lot matches, 10,669 intraday pairs and
60,918 gain lines; 500,000 trades produced 226,987 lots, 343,467 matches, 108,477 intraday
pairs and 317,952 gain lines. The 250k and 500k rows were measured after the other workloads
had been stopped, which is why 500k shows a lower peak heap than 250k (less GC pressure).

## Observations

- **Matching is linear in trades.** FIFO per (account, ISIN) with a sorted queue is
  35-45 µs per trade including the intraday-netting pass (3.55 s at 100k, 21 s at 500k). Decimal arithmetic
  (`shopspring/decimal`) dominates; it is the price of exact money.
- **The full JSON report is the expensive part, not the maths.** The report embeds every
  trade, lot, match and gain with its explanation (157 MB for 100k trades). The API never
  returns this document; it stores gains and lots in Postgres and pages them. The CLI
  writes it on request.
- **The 1M run failed with `VirtualAlloc ... out of memory`** during matching: the test
  machine had under 2 GB free with Docker and the dev servers running, and one million
  decimal-valued trades plus their lots and matches exceed that. Extrapolating the linear
  stages, one million trades is roughly 45 s of matching and 75 s end to end on a machine
  with 4 GB free; the limit on this machine is memory, not time.

## Planned optimisation (Phase 5 follow-ups)

1. Stream the report sections (trades, lots, gains) instead of materialising one
   document; the `pipeline.Report` already keeps them as separate slices.
2. Drop `Trade.Raw` from the engine's working set once normalised; it is only needed for
   the explain drawer's source-row pointer, which stores file and line instead.
3. Batch Postgres writes in `recompute` with `pgx.CopyFrom` (currently one `INSERT` per
   lot and per gain inside a single transaction).

Re-run after each change with:

```sh
cd backend
go run ./cmd/bench -trades 100000,1000000 -accounts 3 -isins 200 -cpuprofile ../.data/pipeline.prof
go tool pprof -top ../.data/pipeline.prof | head -30
```
