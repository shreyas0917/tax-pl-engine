# ADR 0002: FIFO is applied per demat account, never across brokers

**Status:** accepted · **Date:** 2026-10-26

## Context
A user with Zerodha and Groww accounts may hold the same ISIN in both. Which lot does a Groww sale consume?

## Decision
FIFO queues are keyed by (account, ISIN). The matcher runs one independent engine per account; a property test asserts that matching account A alone equals A's slice of matching all accounts together.

## Rationale
CBDT Circular 768 (24 June 1998) states that where an investor has more than one securities account, FIFO is applied account-wise. Brokers (Zerodha, Groww, Upstox) compute their reports the same way, so our consolidated report reconciles line by line with each broker's own statement.

## Consequences
- Off-market transfers between the user's own accounts must be modelled explicitly (`TRANSFER_OUT` / `TRANSFER_IN` trades carrying the original cost and date).
- A sale in an account with no purchase history is a review item, never a silent match against another account's lots.
