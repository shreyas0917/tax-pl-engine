# ADR 0003: Tax rules are versioned data files, not code

**Status:** accepted · **Date:** 2026-10-26

## Context
Rates, thresholds, dates and even section numbers change every budget, and the Income-tax Act 2025 renumbers everything from 1 April 2026. Several rules (the 12-month boundary, the LTCG exemption allocation, intraday pairing) have more than one defensible reading that a CA may want to switch.

## Decision
One YAML file per financial year in `backend/internal/tax/rules/`, embedded into the binary and validated at load (periods must tile the year, boundary enum must be known). Every behavioural choice is a named field (`long_term_boundary`, `ltcg_exemption_allocation`, `intraday.netting`, `buyback.periods[].regime`, `grandfathering.apply_to_bonus_shares`). The engine reads `references` for section names and never hardcodes them. Each report records the `rules_version` it used (`fy2025-26@1`).

## Consequences
- A CA's correction is a data change plus a golden test, and bumps `version`.
- Old reports can be regenerated exactly by loading the recorded version (versions are kept, never edited in place once released).
- The UI and reports show whatever label and law the file says ("FY 2025-26 (AY 2026-27)" vs "Tax Year 2026-27").
