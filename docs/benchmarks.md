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
| 1,000,000 | 3 | 200 | — | — | — | out of memory (see below) | — | — |

100,000 trades produced 50,756 lots, 73,841 lot matches, 10,669 intraday pairs and
60,918 gain lines.

## Observations

- **Matching is linear in trades.** FIFO per (account, ISIN) with a sorted queue is
  ~35 µs per trade including the first intraday-netting pass. Decimal arithmetic
  (`shopspring/decimal`) dominates; it is the price of exact money.
- **The full JSON report is the expensive part, not the maths.** The report embeds every
  trade, lot, match and gain with its explanation (157 MB for 100k trades). The API never
  returns this document; it stores gains and lots in Postgres and pages them. The CLI
  writes it on request.
- **The 1M run failed with `VirtualAlloc ... out of memory`** while the process was at
  roughly 3.5 GB: the report document for one million trades needs more than the ~1 GB
  that was free on the test machine at that moment. The matcher itself completed the
  100k run in 3.6 s, so one million trades is roughly 40 s of matching; the limit is the
  monolithic report, which is a memory problem rather than a time problem.

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
