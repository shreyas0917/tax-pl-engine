// Package parser defines the broker tradebook plugin interface and the registry that
// auto-detects which parser to use. Parsers are pure: bytes in, raw rows and issues out.
package parser

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/shopspring/decimal"

	"github.com/shreyas0917/taxpl/internal/domain"
)

// RawTrade is one tradebook row after the broker-specific parser has typed its columns.
// It carries no account information; normalize attaches that.
type RawTrade struct {
	Line       int               `json:"line"`
	Broker     string            `json:"broker"`
	Symbol     string            `json:"symbol"`
	ISIN       string            `json:"isin"`
	Exchange   string            `json:"exchange"`
	Segment    domain.Segment    `json:"segment"`
	Series     string            `json:"series"`
	Kind       domain.TradeKind  `json:"kind"`
	Side       domain.Side       `json:"side"`
	Quantity   decimal.Decimal   `json:"quantity"`
	Price      decimal.Decimal   `json:"price"`
	TradeDate  domain.Date       `json:"trade_date"`
	TradeTime  string            `json:"trade_time"`
	TradeID    string            `json:"trade_id"`
	OrderID    string            `json:"order_id"`
	Expiry     domain.Date       `json:"expiry"`
	Strike     decimal.Decimal   `json:"strike"`
	OptionType string            `json:"option_type"`
	Charges    *domain.Charges   `json:"charges,omitempty"`
	Raw        map[string]string `json:"-"`
}

// ParseIssue is a rejected or suspicious row. Nothing is ever dropped silently.
type ParseIssue struct {
	Line   int    `json:"line"`
	Reason string `json:"reason"`
	Row    string `json:"row,omitempty"`
}

func (p ParseIssue) String() string { return fmt.Sprintf("line %d: %s", p.Line, p.Reason) }

// Parser is the plugin interface each broker implements (spec Section 8).
type Parser interface {
	Broker() string
	// Detect returns a confidence 0..1 that this file belongs to this broker/format.
	Detect(header []string, sample [][]string) float64
	// Parse returns raw rows with source line numbers; never guesses silently.
	Parse(r io.Reader) ([]RawTrade, []ParseIssue, error)
}

var (
	mu       sync.RWMutex
	registry = map[string]Parser{}
)

// Register adds a parser; called from each broker package's init.
func Register(p Parser) {
	mu.Lock()
	defer mu.Unlock()
	registry[strings.ToLower(p.Broker())] = p
}

// Get returns the parser for a broker name (case-insensitive).
func Get(broker string) (Parser, bool) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := registry[strings.ToLower(broker)]
	return p, ok
}

// Brokers lists registered broker names, sorted.
func Brokers() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// DetectThreshold is the minimum confidence for auto-selecting a parser.
const DetectThreshold = 0.6

// Detection is one parser's score.
type Detection struct {
	Broker     string
	Confidence float64
}

// Detect scores every registered parser against the file's header and first rows.
func Detect(data []byte) []Detection {
	header, sample, err := ReadCSVHead(bytes.NewReader(data), 5)
	if err != nil {
		return nil
	}
	mu.RLock()
	defer mu.RUnlock()
	var out []Detection
	for name, p := range registry {
		out = append(out, Detection{Broker: name, Confidence: p.Detect(header, sample)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		return out[i].Broker < out[j].Broker
	})
	return out
}

// Best returns the best parser if it clears the threshold.
func Best(data []byte) (Parser, Detection, bool) {
	ds := Detect(data)
	if len(ds) == 0 || ds[0].Confidence < DetectThreshold {
		return nil, Detection{}, false
	}
	p, _ := Get(ds[0].Broker)
	return p, ds[0], true
}

// ReadCSVHead reads the header and up to n sample rows.
func ReadCSVHead(r io.Reader, n int) ([]string, [][]string, error) {
	cr := NewCSVReader(r)
	header, err := cr.Read()
	if err != nil {
		return nil, nil, err
	}
	var rows [][]string
	for len(rows) < n {
		rec, err := cr.Read()
		if err != nil {
			break
		}
		rows = append(rows, rec)
	}
	return NormaliseHeader(header), rows, nil
}

// NewCSVReader builds a lenient CSV reader (variable field counts, lazy quotes, BOM stripped).
func NewCSVReader(r io.Reader) *csv.Reader {
	br := &bomReader{r: r}
	cr := csv.NewReader(br)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	cr.TrimLeadingSpace = true
	return cr
}

type bomReader struct {
	r    io.Reader
	done bool
}

func (b *bomReader) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if !b.done && n >= 3 && p[0] == 0xEF && p[1] == 0xBB && p[2] == 0xBF {
		copy(p, p[3:n])
		n -= 3
	}
	b.done = true
	return n, err
}

// NormaliseHeader lower-cases and trims header cells so parsers can match them loosely.
func NormaliseHeader(h []string) []string {
	out := make([]string, len(h))
	for i, c := range h {
		out[i] = strings.ToLower(strings.TrimSpace(strings.Trim(c, "\"")))
	}
	return out
}

// Columns maps normalised header names to indexes.
type Columns map[string]int

// NewColumns indexes a header.
func NewColumns(header []string) Columns {
	c := Columns{}
	for i, h := range NormaliseHeader(header) {
		c[h] = i
	}
	return c
}

// Get returns a cell by column name ("" when absent).
func (c Columns) Get(row []string, names ...string) string {
	for _, n := range names {
		if i, ok := c[n]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
	}
	return ""
}

// Has reports whether every column is present.
func (c Columns) Has(names ...string) bool {
	for _, n := range names {
		if _, ok := c[n]; !ok {
			return false
		}
	}
	return true
}

// ParseDecimal parses a number that may carry commas or a currency sign.
func ParseDecimal(s string) (decimal.Decimal, error) {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer(",", "", "₹", "", "Rs.", "", "Rs", "", " ", "").Replace(s)
	if s == "" || s == "-" {
		return decimal.Zero, fmt.Errorf("empty number")
	}
	return decimal.NewFromString(s)
}

// ParseSide maps broker vocab (buy/sell/B/S/purchase) to a Side.
func ParseSide(s string) (domain.Side, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "BUY", "B", "PURCHASE", "BOUGHT":
		return domain.Buy, nil
	case "SELL", "S", "SOLD", "SALE":
		return domain.Sell, nil
	}
	return "", fmt.Errorf("unknown trade type %q", s)
}
