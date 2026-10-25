# Tax rules implemented

Every rule the engine applies, where it comes from, how it is configured and which test proves it.
Research date: **26 October 2026**. Status legend:

- **VERIFIED (sources)** — confirmed from the statute, a CBDT circular, a court ruling or at least two independent practitioner sources listed below.
- **CA SIGN-OFF PENDING** — implemented per the sources, but the owner's Chartered Accountant has not yet signed the `verified_by` field in the rules file. Nothing in this project is tax advice.

Rules are data, not code: see `backend/internal/tax/rules/*.yaml`. Each job records the rules version it used (for example `fy2025-26@1`).

| # | Rule | Implementation | Golden test(s) | Status |
|---|---|---|---|---|
| 1 | A sale belongs to the financial year (1 Apr - 31 Mar) of its **trade date** (contract-note date, not settlement) — CBDT Circular 704 (1995) | `domain.FinancialYear` | `08_fy_boundary` | VERIFIED |
| 2 | Sales to 31 Mar 2026 fall under the Income-tax Act 1961; from 1 Apr 2026 under the Income-tax Act 2025 ("Tax Year"). Section numbers come from the rules file, never code | `rules/*.yaml` `law`, `references` | `20_buyback_capital_gains` (TY 2026-27) | VERIFIED; new-Act section numbers from ClearTax/BVACA/Taxmann mapping tables, CA sign-off pending |
| 3 | **Holding period**: listed equity / equity MF units held 12 months or more are long-term. The acquisition day counts, so a sale **on the anniversary** is long-term — Delhi HC, *Bharti Gupta Ramola v CIT* (ITA 1234/2011, 12 Apr 2012); s.2(42A) 1961 / s.2(101) 2025 | `tax.IsLongTerm`, `equity.long_term_boundary: on_or_after_anniversary` (alternative `after_anniversary`) | `04_boundary_12_months`, `32_leap_year_boundary`, `08_fy_boundary`, property test `TestPropertyLongTermMonotonic` | VERIFIED |
| 4 | Twelve **calendar** months, clamped to month end (29 Feb 2024 → 28 Feb 2025). Day-count rules (≥365) can be a day off in leap spans | `domain.Date.AddMonths` | `32_leap_year_boundary` | VERIFIED |
| 5 | **FIFO per demat account and per ISIN** — s.45(2A) read with CBDT Circular 768 (24 Jun 1998): "FIFO will be applied account-wise". Never across brokers | `matcher` (`accountEngine` per account) | `06_two_accounts_same_isin`, `02_partial_sell_two_lots`, property tests `TestPropertyFIFOInvariants`, `TestPropertyAccountsAreIndependent` | VERIFIED |
| 6 | **Intraday netting**: same-day buy and sell of one ISIN in one account is speculative to the extent of min(buy, sell) (s.43(5) 1961 / s.66(31) 2025: settled otherwise than by delivery). Excess sells are delivery sales matched FIFO against older lots; excess buys become lots | `matcher.netIntraday`, `intraday.netting: min_buy_sell_per_day` | `07_same_day_intraday`, `09_intraday_with_holdings`, `33_intraday_sell_before_buy`, property `TestPropertyNettingNeverExceedsMinBuySell` | VERIFIED (legal principle); exact within-day pairing order is broker practice, CA sign-off pending |
| 7 | STCG rate 15% / LTCG 10% for transfers up to 22 Jul 2024; 20% / 12.5% from 23 Jul 2024 (Finance (No.2) Act 2024); unchanged by Budget 2026 | `equity.periods` | `22_fy2024_25_rate_split`, `31_split_after_partial_sale` | VERIFIED |
| 8 | **LTCG exemption** ₹1.25 lakh is a single aggregate limit for the whole of FY 2024-25 (not ₹1 lakh pre / ₹1.25 lakh post); ₹1 lakh for FY 2023-24 | `equity.ltcg_exemption`, allocation across rate periods `pro_rata` (not prescribed; labelled estimate) | `22_fy2024_25_rate_split` | VERIFIED (limit); allocation order CA sign-off pending |
| 9 | **Grandfathering** for STT-paid equity acquired on or before 31 Jan 2018: cost = max(actual, min(FMV on 31 Jan 2018, sale value)), per lot on totals — s.55(2)(ac) 1961 / s.90(7)-(8) 2025. FMV = highest traded price that day | `tax.classifyMatch`, `grandfathering.*` | `10_grandfathering_basic`, `11_grandfathering_sale_below_fmv`, `12_grandfathering_actual_cost_higher`, `27_missing_fmv` | VERIFIED |
| 10 | Grandfathered FMV is per **original** share: after a split the per-share FMV is divided by the split factor so total FMV is unchanged | `corpaction.applySplit` | `13_grandfathering_split_after_2018` | VERIFIED (Freefincal worked example; arithmetic follows from the statute) |
| 11 | **Bonus shares** allotted on or before 31 Jan 2018 get the FMV cost too (no carve-out in s.55(2)(ac); ITAT Mumbai, *Ramesh Jaisinghani v DCIT*, ITA 980/Mum/2025) | `grandfathering.apply_to_bonus_shares: true` | `16_bonus_pre_2018_grandfathered` | VERIFIED |
| 12 | **Split**: quantity × ratio, total cost unchanged, acquisition date unchanged | `corpaction.applySplit` | `14_split_basic`, `31_split_after_partial_sale` | VERIFIED |
| 13 | **Bonus**: new lot, cost nil, acquisition date = allotment (ex) date, holding period separate from the original (s.55(2)(aa)(iiia)) | `corpaction.applyBonus` | `15_bonus_basic` | VERIFIED |
| 14 | **Demerger**: cost of resulting-company shares = original cost × announced ratio (s.49(2C)); original cost reduced by the same (s.49(2D)); holding period carries (s.2(42A) Expl.1(g)). Ratio comes from the company's shareholder letter / exchange filing | `corpaction.applyDemerger`, `CostSplitPct` | `17_demerger` | VERIFIED |
| 15 | **Merger / amalgamation**: no transfer (s.47(vii)); cost and holding period carry to the new shares (s.49(2), s.2(42A) Expl.1(c)); quantity × swap ratio | `corpaction.applyMerger` | `18_merger` | VERIFIED |
| 16 | **Buyback, to 30 Sep 2024**: company paid tax u/s 115QA; proceeds exempt u/s 10(34A) — shown, no gain | `buyback.periods` regime `exempt_115qa` | `21_buyback_exempt_115qa` | VERIFIED |
| 17 | **Buyback, 1 Oct 2024 - 31 Mar 2026**: proceeds are deemed dividend u/s 2(22)(f) (other sources); s.46A deems consideration nil so the cost is a capital loss by holding period | regime `deemed_dividend` | `19_buyback_deemed_dividend` | VERIFIED |
| 18 | **Buyback, from 1 Apr 2026**: ordinary capital gains (Finance Act 2026, s.69 of the 2025 Act substituted; s.2(40)(f) omitted). Promoter surcharge out of scope | regime `capital_gains` | `20_buyback_capital_gains` | VERIFIED (ITR reporting for AY 2027-28 not yet notified) |
| 19 | **Off-market transfer between the same person's accounts** is not a transfer (s.2(47)): cost and acquisition date carry to the receiving account; FIFO position follows credit date in that account | `matcher.openTransferLot`, trade kinds `TRANSFER_IN/OUT` | `25_off_market_transfer`, `26_transfer_unknown_details` | VERIFIED |
| 20 | **Charges (capital gains)**: purchase-side brokerage and other non-STT charges are added to cost; sale-side non-STT charges are deducted (s.48 expenses wholly in connection with the transfer). **STT is never deductible** (s.48 proviso) | `matcher.openLot`, `nonSTTShare` | `28_charges_capital_gains` | VERIFIED |
| 21 | **Charges (business income)**: for intraday and F&O every charge including STT is a deductible expense | `matcher.chargesShare` | `07_same_day_intraday`, `23_fno_basic` (charges 0; charge imports arrive with the broker P&L parsers in Phase 4) | VERIFIED |
| 22 | **F&O**: non-speculative business income; P&L per contract FIFO, long or short | `matcher.runFNO` | `23_fno_basic`, `24_fno_partial_fifo` | VERIFIED |
| 23 | **F&O turnover** = Σ absolute P&L of squared-off transactions; option premium not added separately when already inside the net — ICAI Guidance Note on Tax Audit, 2023 ed., para 5.10(b) | `tax.Summarise` (`fno.turnover_method: absolute_pnl`) | `23_fno_basic` | VERIFIED |
| 24 | Tax audit thresholds (s.44AB 1961 / s.63 2025): ₹1 crore; ₹10 crore when cash receipts and payments are each ≤ 5%. The app **never decides** whether an audit is needed | `fno.audit_*`, `Summary.AuditNote` | shown in CLI output | VERIFIED |
| 25 | **Losses**: STCL against STCG/LTCG, 8 years; LTCL against LTCG only, 8 years; speculative against speculative, 4 years; non-speculative business against any head except salary, 8 years; carry-forward needs an on-time return (s.70-74 / s.108-113) | `losses:` in rules, `Summary.Losses` | CLI output on `03_multiple_isins`-style losses; Phase 4 loss-summary tests | VERIFIED |
| 26 | s.87A rebate is not available against s.112A / s.198 LTCG — shown as an information note | `equity.rebate_87a_on_ltcg: false` | — | VERIFIED |
| 27 | **STT rates** (dated): equity delivery 0.1% both sides; intraday sell 0.025%; futures sell 0.02% → **0.05%** from 1 Apr 2026; options sell 0.1% → **0.15%**; options exercise 0.125% → **0.15%** (Finance Act 2026, NSE circular FATAX73524) | `charges` package (Phase 4) | Phase 4 | VERIFIED |

## Decisions taken on the [ASK CA] items (spec 6.12)

The spec left nine questions for a CA. Each was researched (26 Oct 2026) and implemented behind a rules-file setting so the CA can flip it without a code change. The CA's eventual answer goes into `verified_by` and, where different, into a new rules version.

| Spec item | Decision | Basis | Confidence |
|---|---|---|---|
| 1. 12-month boundary | Sale on the anniversary is long-term | Delhi HC, Bharti Gupta Ramola (2012); no contrary HC ruling | High |
| 2. ₹1.25 lakh across the 23 Jul 2024 split | One aggregate limit for the year; allocated pro-rata between buckets for the tax *estimate* only | Finance (No.2) Act 2024 amended s.112A(2) for the whole AY; ClearTax, TaxBuddy | High (limit) / Medium (allocation order) |
| 3. Intraday netting with holdings | min(buy, sell) is intraday; the rest is a delivery sale from the oldest lot | s.43(5); Zerodha/Quicko/Groww report practice | High (principle) / Medium (pairing order) |
| 4. Own-account off-market transfer | Cost and date carry over; FIFO account-wise | s.2(47); Circular 768; Zerodha, HDFC Sky, Quicko | High |
| 5. Pre-2018 bonus grandfathering | FMV applies | s.55(2)(ac) text; ITAT Mumbai, Jaisinghani (Oct 2025) | High |
| 6. Demerger / merger ratios | Company's s.49(2C) letter / exchange filing; entered as reference data | s.49(2C)/(2D), s.47(vii), s.49(2) | High |
| 7. Buyback reporting | Three dated regimes (see rows 16-18) | Finance (No.2) Act 2024; Finance Act 2026 | High |
| 8. 2025-Act section numbers | 2(101), 63, 66(31), 90(7), 108/109, 111-113, 196/198, 69 | ClearTax, BVACA, Taxmann mapping tables; incometaxindia.gov.in section pages | High / Medium for loss sections |
| 9. Grandfathering under the 2025 Act | Continues unchanged (s.90(7)-(8)); no sunset | Act text; Budget 2026 silent | High |

## Sources

Statute, circulars and rulings
- Income-tax Act 1961 s.2(42A), 43(5), 45(2A), 46A, 47, 48, 49, 55(2)(ac), 70-74, 111A, 112A, 115QA; Income-tax Act 2025 s.2(101), 63, 66, 69, 90, 108-113, 196-198 — https://www.incometaxindia.gov.in/w/section-90-114 , https://www.incometaxindia.gov.in/w/section-113-98 , https://www.taxsutra.com/lsi/act/section/print/146588
- CBDT Circular 768 (24 Jun 1998), FIFO account-wise — https://incometaxindia.gov.in/communications/circular/910110000000000355.htm
- CBDT Circular 704 (28 Apr 1995), date of transfer for listed shares — https://www.incometaxindia.gov.in/w/704-circular-no.-704-dated-28-4-1995
- Delhi HC, Bharti Gupta Ramola v CIT, ITA 1234/2011 — https://delhihighcourt.nic.in/app/downloadOrderbByDate/ITA/1234/2011/12-04-2012 ; digest https://bcajonline.org/journal/capital-gains-long-term-short-term-s-s-242a-1038-and-54ec-a-y-2006-07-period-of-holding-if-an-assessee-acquires-an-asset-on-2nd-january-in-the-preceding-year-the-period-of-12-months-woul/
- ITAT Mumbai, Ramesh Jaisinghani v DCIT (bonus shares, s.55(2)(ac)) — https://indiankanoon.org/doc/191940901/
- ICAI Guidance Note on Tax Audit (2023) — https://kb.icai.org/pdfs/PDFFile664ad875b49bb0.41546228.pdf
- NSE circular FATAX73524 (STT from 1 Apr 2026) — https://nsearchives.nseindia.com/content/circulars/FATAX73524.pdf

Practitioner sources
- ClearTax LTCG on shares — https://cleartax.in/s/long-term-capital-gains-on-shares ; https://cleartax.in/s/long-term-capital-gains-ltcg-tax ; section mapping https://cleartax.in/s/income-tax-act-2025-section-numbers-old-vs-new ; STT https://cleartax.in/s/securities-transaction-tax-stt ; s.44AB https://cleartax.in/s/tax-audit-section-44ab
- TaxBuddy, 23 Jul 2024 change — https://www.taxbuddy.com/blog/new-capital-gain-tax
- Taxmann, F&O turnover — https://www.taxmann.com/post/blog/tax-audit-how-to-calculate-the-turnover-in-the-case-of-derivatives/ ; Finance Bill 2026 amendments https://www.taxmann.com/post/blog/taxmanns-analysis-finance-bill-amendments-passed-by-lok-sabha
- Treelife, buyback tax regimes — https://treelife.in/taxation/buyback-tax-in-india/ ; TaxGuru https://taxguru.in/income-tax/buy-taxation-finance-act-2026-resettling-unsettledae.html
- Freefincal, grandfathering with splits — https://freefincal.com/how-to-calculate-ltcg-with-grandfathering-for-equity-shares-that-split-how-to-fill-schedule-112a/
- Geojit, demerger — https://support.geojit.com/support/solutions/articles/89000006866 ; bonus https://support.geojit.com/support/solutions/articles/89000006864 ; grandfathering https://support.geojit.com/support/solutions/articles/89000006851
- Zerodha: intraday classification https://support.zerodha.com/category/console/portfolio/holdings/articles/multiple-trades-discrepant ; tax reports https://zerodha.com/z-connect/taxation-for-traders/zerodha-tax-reports ; secondary demat / Circular 768 https://zerodha.com/z-connect/featured/manage-your-investments-better-with-secondary-demat-account ; turnover https://zerodha.com/varsity/chapter/turnover-balance-sheet-and-pl/
- KPMG Union Budget 2026-27 note (rate card) — https://assets.kpmg.com/content/dam/kpmgsites/in/pdf/2026/02/india-union-budget-2026-27-sovereign-wealth-funds-and-pension-funds.pdf
- Tata MF post-Budget 2026 — https://www.tatamutualfund.com/blogs/post-budget-2026-new-tax-map-equity-investors
- Business Standard, multiple demat FIFO — https://www.business-standard.com/finance/personal-finance/explained-how-to-calculate-capital-gains-tax-on-multiple-demat-accounts-124080200149_1.html
- 1 Finance, F&O turnover — https://1finance.co.in/blog/fo-turnover-calculation-income-tax/
- Losses: m.Stock — https://www.mstock.com/articles/short-term-vs-long-term-capital-loss-set-off-carry-forward ; Upstox — https://upstox.com/market-talk/set-off-and-carry-forward-of-losses-on-shares/
- Court Kutchehry, brokerage and STT treatment — https://courtkutchehry.com/blog/capital-gains-taxation-brokerage-stt-investment-deductions

## What the CA still needs to sign

1. Confirm each row above and fill `verified_by` in every rules file.
2. Provide hand-worked answers for five example trades (`ca_example_a` … `ca_example_e` golden cases).
3. Confirm the allocation of the annual LTCG exemption across the FY 2024-25 rate buckets as the ITR utility applies it.
