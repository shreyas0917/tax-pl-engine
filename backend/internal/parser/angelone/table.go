package angelone

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/parser"
)

// headerScan is how many leading rows may precede the header (client name, report title, ...).
const headerScan = 40

// tradesSheet is the worksheet Angel One writes the trades to; the first sheet is used otherwise.
const tradesSheet = "TradesAndCharges"

// row is one source row with its real CSV line or 1-based sheet row number.
type row struct {
	line  int
	cells []string
}

var (
	zipMagic = []byte("PK\x03\x04")
	oleMagic = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}
)

// readRows loads an XLSX workbook (first sheet) or a CSV file. Malformed CSV lines are
// returned as issues so nothing is dropped silently.
func readRows(r io.Reader) ([]row, []parser.ParseIssue, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, fmt.Errorf("read: %w", err)
	}
	switch {
	case bytes.HasPrefix(data, zipMagic):
		rows, err := readXLSX(data)
		return rows, nil, err
	case bytes.HasPrefix(data, oleMagic):
		return nil, nil, errors.New("workbook is password protected or a legacy .xls: open it (password = PAN in capitals) and save an unprotected .xlsx or .csv copy")
	}
	rows, issues := readCSV(data)
	return rows, issues, nil
}

func readXLSX(data []byte) ([]row, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer func() { _ = f.Close() }()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, errors.New("xlsx has no sheets")
	}
	sheet := sheets[0]
	for _, s := range sheets {
		if strings.EqualFold(s, tradesSheet) {
			sheet = s
		}
	}
	raw, err := f.GetRows(sheet, excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, fmt.Errorf("read sheet %q: %w", sheet, err)
	}
	rows := make([]row, 0, len(raw))
	for i, cells := range raw {
		rows = append(rows, row{line: i + 1, cells: cells})
	}
	return rows, nil
}

func readCSV(data []byte) ([]row, []parser.ParseIssue) {
	cr := parser.NewCSVReader(bytes.NewReader(data))
	var (
		rows   []row
		issues []parser.ParseIssue
	)
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			line := 0
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				line = pe.Line
			}
			issues = append(issues, parser.ParseIssue{Line: line, Reason: "malformed CSV row: " + err.Error()})
			continue
		}
		line, _ := cr.FieldPos(0) // real source line, blank lines included
		rows = append(rows, row{line: line, cells: rec})
	}
	return rows, issues
}

// findHeader returns the index of the first row (within headerScan) carrying every required column.
func findHeader(rows []row, required []string) int {
	for i, r := range rows {
		if i >= headerScan {
			break
		}
		if parser.NewColumns(r.cells).Has(required...) {
			return i
		}
	}
	return -1
}

func isBlank(cells []string) bool {
	for _, c := range cells {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// parseDateTime accepts "13-09-2021 02:13 PM", "2024-06-03 10:15:22", ISO "T" stamps, bare dates
// in every layout domain.ParseDate knows, and raw Excel serial numbers. The clock is "" when absent.
func parseDateTime(s string) (domain.Date, string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return domain.Date{}, "", errors.New("empty date")
	}
	if serial, err := strconv.ParseFloat(s, 64); err == nil {
		t, err := excelize.ExcelDateToTime(serial, false)
		if err != nil {
			return domain.Date{}, "", fmt.Errorf("excel date %q: %w", s, err)
		}
		t = t.Round(time.Second)
		clock := ""
		if serial != float64(int64(serial)) {
			clock = t.Format("15:04:05")
		}
		return domain.DateOf(t), clock, nil
	}
	if d, err := domain.ParseDate(s); err == nil {
		return d, clockPart(s), nil
	}
	i := strings.IndexAny(s, " T")
	if i <= 0 {
		return domain.Date{}, "", fmt.Errorf("unrecognised date %q", s)
	}
	d, err := domain.ParseDate(s[:i])
	if err != nil {
		return domain.Date{}, "", err
	}
	return d, parseClock(s[i+1:]), nil
}

var clockLayouts = []string{"15:04:05", "15:04", "03:04:05 PM", "03:04 PM", "3:04:05 PM", "3:04 PM", "15:04:05.000"}

// parseClock normalises "02:13 PM" / "14:13:05" to "15:04:05"; unknown shapes give "".
func parseClock(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	for _, l := range clockLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.Format("15:04:05")
		}
	}
	return ""
}

func clockPart(s string) string {
	if i := strings.IndexAny(s, " T"); i >= 0 && i+1 < len(s) {
		return parseClock(s[i+1:])
	}
	return ""
}
