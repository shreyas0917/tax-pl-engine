package report_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/shreyas0917/taxpl/internal/domain"
	_ "github.com/shreyas0917/taxpl/internal/parser/all"
	"github.com/shreyas0917/taxpl/internal/pipeline"
	"github.com/shreyas0917/taxpl/internal/report/jsonreport"
	"github.com/shreyas0917/taxpl/internal/report/pdfreport"
	"github.com/shreyas0917/taxpl/internal/report/xlsxreport"
)

// loadGolden runs a golden case so the report writers are exercised on realistic data.
func loadGolden(t *testing.T, name string) *pipeline.Report {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata", "golden", name)
	b, err := os.ReadFile(filepath.Join(dir, "case.json"))
	require.NoError(t, err)
	var cf struct {
		FY    string `json:"fy"`
		Files []struct {
			Name    string `json:"name"`
			Account string `json:"account"`
		} `json:"files"`
		Instruments      []domain.Instrument      `json:"instruments"`
		CorporateActions []domain.CorporateAction `json:"corporate_actions"`
	}
	require.NoError(t, json.Unmarshal(b, &cf))
	req := pipeline.Request{FY: cf.FY, Instruments: map[string]domain.Instrument{}, CorporateActions: cf.CorporateActions}
	for _, i := range cf.Instruments {
		req.Instruments[i.ISIN] = i
	}
	for _, f := range cf.Files {
		data, err := os.ReadFile(filepath.Join(dir, "input", f.Name))
		require.NoError(t, err)
		req.Files = append(req.Files, pipeline.File{Name: f.Name, AccountID: f.Account, Data: data})
	}
	rep, err := pipeline.Run(req)
	require.NoError(t, err)
	return rep
}

func TestXLSXReport(t *testing.T) {
	rep := loadGolden(t, "22_fy2024_25_rate_split")
	data, ct, err := xlsxreport.Write(rep, "2024-25")
	require.NoError(t, err)
	require.Equal(t, xlsxreport.ContentType, ct)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"Summary", "Gains", "Intraday", "F&O", "Holdings", "Review", "Trades"}, f.GetSheetList())
	rows, err := f.GetRows("Gains")
	require.NoError(t, err)
	require.Len(t, rows, 3, "header + two LTCG lines")
	require.Equal(t, "LTCG_EQUITY", rows[1][1])
	require.Equal(t, "150,000.00", rows[1][13], "numeric cell with INR grouping")
	sum, err := f.GetRows("Summary")
	require.NoError(t, err)
	var joined string
	for _, r := range sum {
		for _, c := range r {
			joined += c + "|"
		}
	}
	require.Contains(t, joined, "fy2024-25@1")
	require.Contains(t, joined, "not tax advice")
	require.Contains(t, joined, "Data fingerprint")
	require.Contains(t, joined, "2024-07-23..2025-03-31")

	// Determinism of the content (excelize embeds timestamps, so compare cell data via re-render).
	data2, _, err := xlsxreport.Write(rep, "2024-25")
	require.NoError(t, err)
	f2, _ := excelize.OpenReader(bytes.NewReader(data2))
	rows2, _ := f2.GetRows("Gains")
	require.Equal(t, rows, rows2)
}

func TestPDFReport(t *testing.T) {
	rep := loadGolden(t, "13_grandfathering_split_after_2018")
	data, ct, err := pdfreport.Write(rep, "2025-26")
	require.NoError(t, err)
	require.Equal(t, pdfreport.ContentType, ct)
	require.True(t, bytes.HasPrefix(data, []byte("%PDF-")))
	require.Greater(t, len(data), 2000)
	b, _ := jsonreport.Marshal(rep)
	require.NotEmpty(t, jsonreport.Checksum(b))
}
