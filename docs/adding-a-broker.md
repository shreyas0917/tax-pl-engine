# Adding a broker parser

Broker tradebook parsers live in `backend/internal/parser/<broker>/`. They are pure plugins:
bytes in, `RawTrade` rows and `ParseIssue`s out. The core engine never learns a broker's name;
it only talks to the registry in `backend/internal/parser/parser.go`.

## 1. The interface

```go
type Parser interface {
    Broker() string
    // Detect returns a confidence 0..1 that this file belongs to this broker/format.
    Detect(header []string, sample [][]string) float64
    // Parse returns raw rows with source line numbers; never guesses silently.
    Parse(r io.Reader) ([]RawTrade, []ParseIssue, error)
}
```

Each package exposes

```go
const Broker = "<name>"           // lower-case registry key, also used in account mapping
type Parser struct{}
func init() { parser.Register(Parser{}) }
```

and `backend/internal/parser/all/all.go` gets one blank import line. That is the only change
outside the new package.

## 2. Parse rules

* Convert **every** data row into a `parser.RawTrade` or a `parser.ParseIssue` carrying the real
  source line (`cr.FieldPos(0)` for CSV, the 1-based sheet row for XLSX). Nothing is dropped
  silently; cancelled orders, bad numbers, unknown segments and undecodable contracts all become
  issues. Preamble rows above the header (client name, report title) are not data rows.
* Return a non-nil `error` only when the file is not this broker's export at all (header missing).
* Money and quantities are `shopspring/decimal` (`parser.ParseDecimal` strips commas and `₹`).
  Never use floats.
* Sides go through `parser.ParseSide` (buy/sell/B/S/purchase/sold...).
* Dates go through `domain.ParseDate` (or a wrapper that also accepts "dd-mm-yyyy hh:mm AM" and
  Excel serial numbers, see `groww/table.go`). `TradeTime` is "15:04:05" when known.
* Segments: equity rows are `domain.SegEquity` with the ISIN when the export has one; leave ISIN
  empty when it does not (the normaliser resolves it by symbol and otherwise raises a review
  item). Futures are `SegFuture` with `Expiry`; options are `SegOption` with `Expiry`, `Strike`
  and `OptionType` CE/PE. The `Symbol` of a derivative is the underlying. Set
  `Kind = domain.KindRegular`.
* Fill `TradeID`/`OrderID` from the trade/order number columns when present, `Charges` when the
  export has charge columns (`Source: "imported"`), and `Raw` with every original column keyed
  by its normalised header.
* XLSX exports: read with `github.com/xuri/excelize/v2`, convert the sheet to `[][]string`
  (`GetRows(sheet, excelize.Options{RawCellValue: true})`) and run the same column logic as for
  CSV. Sniff the format from the bytes (`PK\x03\x04` = xlsx; the OLE magic means an encrypted
  workbook or legacy .xls, which we refuse with a clear message).

## 3. Detect scoring

The registry reads the first line as the header and the next five rows as `sample`, calls every
parser's `Detect`, and auto-selects the best one if it clears `parser.DetectThreshold` (0.6).
Score by the broker's *distinctive* column set, weighted so that

* the broker's own file scores >= 0.9, and
* every other registered broker's file scores < 0.3.

Generic columns that many brokers share (`quantity`, `price`, `exchange`, `segment`, `isin`,
`date`) should carry little weight; the broker-specific spellings (`scrip/contract`,
`execution date and time`, `trade no`) carry most of it. When the real export has preamble
rows, score the sample rows as well as the header so the hidden header is still found.
`angelone/cross_test.go` runs every sample through the full registry to keep this true.

## 4. Tests and sample file

Every package has `<broker>_test.go` with a synthetic sample (3-6 rows including bad rows)
covering Parse, Detect against its own and foreign headers, F&O decoding where applicable, and an
XLSX round trip built with excelize in the test; plus `testdata/sample.csv` (5 synthetic rows)
exercised through `parser.Best`. Build the UTF-8 BOM from bytes in tests, never as a literal.
Run

```
cd backend
go test ./internal/parser/... && go vet ./internal/parser/...
golangci-lint run ./internal/parser/...
```

## 5. Supported exports

| Broker | Export (menu path) | Format | Columns used | Sources |
|---|---|---|---|---|
| **zerodha** | Console > Reports > Tradebook > CSV (equity and F&O) | CSV | `symbol, isin, trade_date, exchange, segment, series, trade_type, auction, quantity, price, trade_id, order_id, order_execution_time` (+ `expiry_date` for F&O; symbol `NIFTY24APR22500CE`, `BANKNIFTY24MAYFUT`) | Zerodha Console |
| **groww** | Profile > Reports > Transactions (transaction report) > Order history (stocks) > date range > Download ("Stocks Order History") | XLSX (older exports password protected with the PAN); same columns accepted as CSV | preamble rows (`Name`, `Unique Client Code`, "Order history for stocks from ... to ..."), then `Stock name, Symbol, ISIN, Type (BUY/SELL), Quantity, Value, Exchange, Exchange Order Id, Execution date and time (13-09-2021 02:13 PM), Order status (Executed)`. Price = Value / Quantity (a `Price` column is used when present). Non-executed statuses become issues. | [finSights PR #114 fixture](https://github.com/ash4691-rgb/finSights/pull/114), [PortfolioTracker groww.ts](https://github.com/vinitgrwl/PortfolioTracker/blob/main/lib/parsers/groww.ts), [TradesViz Groww guide](https://www.tradesviz.com/how-to-export-trades/groww/), [Groww help: transaction history](https://groww.in/help/my-account/ma-others/where-can-i-get-the-transaction-history) |
| **upstox** | account.upstox.com > Reports > Trade (`https://account.upstox.com/reports/trade`), or app: Upstox icon > My account > Reports > Trade report > segment + FY/date range > Get report > Download Excel/XLS or CSV | XLSX or CSV (legacy .xls must be re-saved) | Matched by synonym: `Trade Date|Date`, `Scrip Name|Symbol|Trading Symbol|Instrument`, `ISIN`, `Exchange`, `Segment (EQ/FO/CD/COM/MF)`, `Trade Type|Buy/Sell|Side|Transaction Type`, `Quantity|Qty`, `Price|Trade Price`, `Trade No|Trade Num|Trade ID`, `Order No|Order ID`, `Trade Time`, `Expiry|Expiry Date`, `Strike|Strike Price`, `Option Type (CE/PE)`. F&O typed from the contract columns, else decoded from the name. | [Upstox help: trade reports](https://upstox.com/help-center/t-249884/), [MProfit: Upstox equity trades](https://help.mprofit.in/support/solutions/articles/82000889765), [MProfit: Upstox F&O trades](https://help.mprofit.in/support/solutions/articles/82000889811), [Upstox trade-history API vocabulary](https://upstox.com/developer/api-documentation/get-historical-trades/), [TradesViz Upstox guide](https://tradesviz.crisp.help/en/article/how-to-import-trades-from-upstox-to-tradesviz-step-by-step-guide-hm4vn0/) |
| **angelone** | App > Account > Trades and charges > date range > Download trade history ("Trade History / Sauda Summary") | XLSX with sheet `TradesAndCharges` (XLS/CSV also offered; same columns accepted as CSV) | preamble (`ClientCode`), then `Scrip/Contract, Buy/Sell, Buy Price, Sell Price, Quantity, Brokerage, Order Type (Delivery/Intraday/...), Segment (Capital = cash equity), Date, GST, STT, SEBI Tax, Exchange Turnover Charges, Stamp Duty, Other Charges, IPFT charges`. No ISIN. Buys take Buy Price, sells Sell Price. Charges imported (IPFT + Other folded into ExchangeFee). F&O rows decoded from `Scrip/Contract` (`NIFTY 27JUN24 23000 CE`, `BANKNIFTY 26JUN2024 FUT`) or from `Expiry Date / Strike Price / Option Type` columns when present. | [Angel One support: Trade History (Sauda Summary)](https://www.angelone.in/support/reports-and-statements/trade-history-sauda-summary), [PortfolioTracker angelone.ts](https://github.com/vinitgrwl/PortfolioTracker/blob/main/lib/parsers/angelone.ts), [Arthveda Angel One guide](https://arthveda.app/brokers/angel-one), [Quicko: Angel trade book](https://learn.quicko.com/download-trade-book-angel-broking) |

### Known uncertainty

* **Upstox** publishes no header list for the trade report and the equity and F&O files differ,
  hence the synonym matching. If a real file uses a spelling not in `upstox.go`'s `colSet`s,
  add it there (and to `detectGroups` if it is distinctive).
* **Angel One** segment labels other than `Capital` and the exact spelling of F&O contracts are
  inferred from third-party descriptions; unrecognised segment labels are reported as issues
  rather than guessed. The contract decoder accepts the common spaced, dashed and glued forms.
* **Groww** exports before 2022 may use a different sheet layout; the parser scans the first 40
  rows for the header, so a longer preamble is fine, but renamed columns are not.

The decoders for F&O names (`contract.go` in each package) understand
`NIFTY 27JUN24 23000 CE`, `NIFTY27JUN2423000CE`, `NIFTY 27-JUN-2024 CE 23000`,
`BANKNIFTY 26JUN2024 FUT`, `BAJAJ-AUTO 27 JUN 2024 FUT` and the monthly NSE trading-symbol
style (`NIFTY24JUN23000CE`, `BANKNIFTY24JUNFUT`) when an expiry column supplies the day.
