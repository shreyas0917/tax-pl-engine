# Tax P&L Engine

Consolidates tradebooks from several Indian brokers (Zerodha, Groww, Upstox, Angel One) into one FIFO-matched, fully explained capital-gains and business-income report for the ITR.

> **Status:** Phase 0 (project setup). See [TAXPL_BUILD_SPEC.md](TAXPL_BUILD_SPEC.md) for the full build plan.

## The problem

Traders in India commonly use two or three broker apps. Each gives its own tax P&L and none of them combine. At filing time the user has to work out which gains are short-term, which are long-term, which trades were intraday or F&O, what cost applies to shares bought before 1 Feb 2018, and what happened after splits, bonuses and demergers. This tool does that work and shows how every number was reached.

## Run locally

```sh
cp .env.example .env
make up        # Postgres + Redis in Docker
make migrate
make api       # in one terminal
make worker    # in another
make web       # frontend
```

## Disclaimer

This tool helps organise data. It is **not tax advice**. Verify every figure with a qualified Chartered Accountant before filing.

## Author

**Shreyas Wani** — [GitHub](https://github.com/shreyas0917)
