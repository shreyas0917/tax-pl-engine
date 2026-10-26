# Edge cases solved

Each case below is a real-world trap, the wrong answer that is commonly produced, the right answer with its source, and the golden test that locks it in (`backend/testdata/golden/<case>`).

## 1. Grandfathered cost after a post-2018 split
**Trap:** 100 shares bought in 2017 at ₹300 with FMV ₹605 on 31 Jan 2018 split 1:2 in 2019. Several broker statements multiply the unsplit FMV (₹605) by the post-split quantity (200) and report a cost of ₹1,21,000, turning a ₹19,500 gain into a ₹41,000 "loss".
**Right answer:** FMV is per original share; after the split it is ₹302.50 per share, total FMV ₹60,500. Cost = max(30,000, min(60,500, 80,000)) = ₹60,500; LTCG ₹19,500.
**Test:** `13_grandfathering_split_after_2018`.

## 2. Intraday netting when you already hold the stock
**Trap:** You hold 100 shares; on one day you buy 50 and sell 80. Treating all 80 as a delivery sale (FIFO from the old lot) overstates long-term gains and hides speculative income; treating all 80 as intraday is impossible.
**Right answer:** min(50, 80) = 50 is speculative (s.43(5)); the other 30 are a delivery sale from the oldest lot.
**Test:** `09_intraday_with_holdings`.

## 3. Sale on the 12-month anniversary
**Trap:** Bought 10 Aug 2024, sold 10 Aug 2025. Day-count tools (at least 365 days) and "12 months plus one day" readings disagree.
**Right answer:** Long-term (Delhi HC, Bharti Gupta Ramola). A leap-day purchase (29 Feb 2024) completes 12 months on 28 Feb 2025.
**Tests:** `04_boundary_12_months`, `32_leap_year_boundary`.

## 4. The same ISIN in two demat accounts
**Trap:** Pooling lots across brokers would let a Groww sale consume an older, cheaper Zerodha lot and misstate both the gain and the holding period.
**Right answer:** FIFO per account (Circular 768). Account B's sale consumes B's lot; A's lot stays open.
**Test:** `06_two_accounts_same_isin`.

## 5. One sale spanning a long-term and a short-term lot
A sale of 15 shares takes 10 from a 13-month-old lot (LTCG) and 5 from a 4-month-old lot (STCG). Two gain lines, two categories.
**Test:** `34_mixed_ltcg_stcg_one_sale`.

## 6. Bonus shares straddling the grandfathering date
Bonus shares allotted in 2017 have nil cost, but s.55(2)(ac) still grants them the 31 Jan 2018 FMV as cost (ITAT Mumbai, Jaisinghani, 2025). Bonus shares allotted later have nil cost and start their own holding period.
**Tests:** `16_bonus_pre_2018_grandfathered`, `15_bonus_basic`.

## 7. Buyback: three regimes in three years
Up to 30 Sep 2024 the proceeds are exempt (company paid s.115QA tax). From 1 Oct 2024 the whole proceeds are deemed dividend and the cost becomes a capital loss (s.46A). From 1 Apr 2026 it is an ordinary capital gain again (Finance Act 2026). The engine picks the regime from the buyback date.
**Tests:** `21_buyback_exempt_115qa`, `19_buyback_deemed_dividend`, `20_buyback_capital_gains`.

## 8. The FY 2024-25 rate change on 23 July 2024
Gains are split by transfer date into 10%/15% and 12.5%/20% buckets, but there is one ₹1.25 lakh exemption for the whole year, not ₹1 lakh before and ₹1.25 lakh after.
**Test:** `22_fy2024_25_rate_split`.

## 9. Demerger and merger keep the original date
Resulting-company shares inherit a share of the cost (s.49(2C)) and the original acquisition date, so a sale a month after listing can still be long-term. Merged shares carry cost and date across the ISIN change.
**Tests:** `17_demerger`, `18_merger`.

## 10. Off-market transfers between your own accounts
Not a transfer under s.2(47): no gain in the sending account, and the receiving account's lot keeps the original cost and date. When those are unknown the engine asks rather than guessing.
**Tests:** `25_off_market_transfer`, `26_transfer_unknown_details`.

## 11. Brokerage yes, STT no
Buy-side brokerage is added to cost and sell-side brokerage is deducted from sale value; STT is never deductible for capital gains (s.48 proviso) but is fully deductible for intraday and F&O business income.
**Test:** `28_charges_capital_gains`.

## 12. A sale with no purchase on file
Shares bought years ago on another platform must not become a zero-cost gain. The engine raises a review item with the quantity and sale date and leaves the gain out until the user supplies the lot or an older tradebook.
**Tests:** `05_sell_without_buy`, `30_opening_lot`.

## 13. Uploading the same file twice
Every trade has a content-derived ID, so a duplicate upload (or an overlapping export) adds nothing.
**Test:** `29_duplicate_upload`; Phase 2 adds the database-level test.

## 14. Short-first intraday and F&O short positions
A sell at 10:00 and a buy at 14:00 with no holdings is an intraday pair, not a missing-purchase error. In F&O a sell opens a short position that a later buy closes FIFO.
**Tests:** `33_intraday_sell_before_buy`, `23_fno_basic`, `24_fno_partial_fifo`.

## 15. Tradebooks without charges
Most tradebook exports carry no brokerage, STT or levies. Rather than silently ignoring them, the engine can estimate them from a dated schedule (STT on futures and options changed on 1 Oct 2024 and again on 1 Apr 2026), labels every estimated value as such, keeps STT out of capital-gains cost and includes it for business income. Intraday legs get the intraday STT rate; the delivery remainder of a partly squared-off order gets the delivery rate.
**Tests:** `35_estimated_charges`, `charges_test.go`.

## 16. The broker says you hold 100, the engine says 94
Holdings reconciliation compares the broker's current holdings statement with the engine's open lots per account and names each difference: missing purchase history, a sale or transfer the uploads do not contain, or an average price that disagrees by more than 1%. It is how a user finds out that an old tradebook is missing before the numbers go in the ITR.
**Tests:** `reconcile_test.go`; API `POST /holdings/reconcile`.

## 17. One tenant must never see another
Every user-owned table has row-level security forced on; the API switches to a non-owner role and binds each request's connection to `app.user_id`, the worker sets it per transaction, and the audit log is append-only for that role. The test writes two users, then proves user A cannot read, count or write user B's rows and cannot delete audit entries.
**Test:** `TestIntegrationRowLevelSecurity`.
