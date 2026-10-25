// Package normalize converts broker-specific raw rows into domain.Trade values bound to a
// demat account, resolving ISINs against reference data. Pure.
package normalize

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/parser"
)

// Input to Normalize.
type Input struct {
	AccountID   string
	Broker      string
	SourceFile  string
	Rows        []parser.RawTrade
	Instruments map[string]domain.Instrument // by ISIN
	// SymbolToISIN resolves rows whose ISIN column is empty (some brokers omit it).
	SymbolToISIN map[string]string
}

// Output of Normalize.
type Output struct {
	Trades []domain.Trade
	Review []domain.ReviewItem
	// Duplicates counts rows whose source hash was already seen in this input.
	Duplicates int
}

// Normalize maps rows to trades. Rows with an unresolvable ISIN become review items and are
// left out of matching; out-of-scope segments are kept as trades (the classifier reports them).
func Normalize(in Input) Output {
	var out Output
	seen := map[string]bool{}
	unknown := map[string]bool{}
	for _, r := range in.Rows {
		t := domain.Trade{
			AccountID: in.AccountID, Broker: in.Broker, ISIN: r.ISIN, Symbol: r.Symbol, Segment: r.Segment, Kind: r.Kind,
			Side: r.Side, Quantity: r.Quantity, Price: r.Price, TradeDate: r.TradeDate, TradeTime: r.TradeTime,
			Exchange: r.Exchange, OrderID: r.OrderID, TradeRef: r.TradeID, SourceFile: in.SourceFile, SourceRow: r.Line,
			Expiry: r.Expiry, Strike: r.Strike, OptionType: r.OptionType, Charges: r.Charges, Raw: r.Raw,
		}
		if t.Kind == "" {
			t.Kind = domain.KindRegular
		}
		if t.Segment.IsEquityLike() {
			if t.ISIN == "" {
				if isin, ok := in.SymbolToISIN[strings.ToUpper(t.Symbol)]; ok {
					t.ISIN = isin
				}
			}
			if t.ISIN == "" {
				if !unknown[t.Symbol] {
					unknown[t.Symbol] = true
					out.Review = append(out.Review, domain.ReviewItem{
						ID: deriveID("review-isin", in.AccountID, t.Symbol), Kind: domain.ReviewUnknownISIN, AccountID: in.AccountID, Symbol: t.Symbol,
						Message: fmt.Sprintf("No ISIN is known for symbol %s in file %s. Enter its ISIN so trades can be matched across symbol changes.", t.Symbol, in.SourceFile),
						Payload: map[string]string{"symbol": t.Symbol, "source_row": fmt.Sprint(r.Line)},
					})
				}
				continue
			}
			if inst, ok := in.Instruments[t.ISIN]; ok && inst.Symbol != "" {
				t.Symbol = inst.Symbol
			}
		}
		// Identity: the broker's trade ID when present, otherwise the full row content.
		if t.TradeRef != "" {
			t.ID = domain.SourceRowHash(in.Broker, in.AccountID, t.TradeRef, string(t.Side), t.ISIN, t.Symbol, t.TradeDate.String())
		} else {
			t.ID = domain.SourceRowHash(in.Broker, in.AccountID, t.InstrumentKey(), string(t.Side), t.Quantity.String(), t.Price.String(), t.TradeDate.String(), t.TradeTime, t.OrderID, fmt.Sprint(r.Line))
		}
		if seen[t.ID] {
			out.Duplicates++
			continue
		}
		seen[t.ID] = true
		out.Trades = append(out.Trades, t)
	}
	sort.SliceStable(out.Review, func(i, j int) bool { return out.Review[i].ID < out.Review[j].ID })
	return out
}

func deriveID(parts ...string) string {
	h := sha256.New()
	h.Write([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h.Sum(nil))[:24]
}
