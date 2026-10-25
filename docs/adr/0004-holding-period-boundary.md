# ADR 0004: A sale on the 12-month anniversary is long-term

**Status:** accepted (CA sign-off pending) · **Date:** 2026-10-26

## Context
s.2(42A) (1961) / s.2(101) (2025) make a listed share long-term when held "for more than twelve months immediately preceding the date of its transfer". Two readings exist: count the acquisition day (anniversary is long-term) or exclude it (anniversary is still short-term). Broker tools often use raw day counts (at least 365), which drift by a day across leap years.

## Decision
Default `long_term_boundary: on_or_after_anniversary`: `sale >= acquisition + 12 calendar months`, with month-end clamping (29 Feb 2024 becomes 28 Feb 2025). The alternative `after_anniversary` is one line in the rules file.

## Rationale
Delhi High Court, *Bharti Gupta Ramola v CIT* (ITA 1234/2011, 12 April 2012): the acquisition date is included, twelve months complete on the day before the anniversary, and a sale on the anniversary is long-term. The Revenue argument of "12 months plus one day" was rejected; no contrary High Court ruling was found. The 2025 Act keeps the same wording.

## Consequences
Golden cases 04, 08 and 32 pin the boundary; `TestPropertyLongTermMonotonic` checks that the rule is monotonic and agrees with day counts away from the boundary.
