package httpapi

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/reconcile"
	"github.com/shreyas0917/taxpl/internal/store"
)

// reconcileHoldings compares a broker holdings statement (multipart: file, account_id) with
// the engine's open lots for that account and reports every difference with a suggestion.
func (s *Server) reconcileHoldings(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadBytes)
	if err := r.ParseMultipartForm(s.cfg.MaxUploadBytes); err != nil {
		writeProblem(w, r, badRequest("expected multipart/form-data with a file field: "+err.Error()))
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		writeProblem(w, r, validation(map[string]string{"file": "a holdings CSV is required"}))
		return
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	accountID := strings.TrimSpace(r.FormValue("account_id"))
	uid := UserID(r.Context())
	if _, err := s.qry(r).GetAccount(r.Context(), store.GetAccountParams{ID: accountID, UserID: uid}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeProblem(w, r, validation(map[string]string{"account_id": "no such account"}))
			return
		}
		writeProblem(w, r, err)
		return
	}
	holdings, issues, err := reconcile.ParseHoldings(bytes.NewReader(data))
	if err != nil {
		writeProblem(w, r, validation(map[string]string{"file": err.Error()}))
		return
	}
	rows, err := s.qry(r).ListOpenLots(r.Context(), uid)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	lots := make([]domain.Lot, 0, len(rows))
	for _, l := range rows {
		lots = append(lots, domain.Lot{AccountID: l.AccountID, ISIN: l.Isin, Symbol: l.Symbol, QuantityOpen: l.QuantityOpen, CostOpen: l.CostOpen, QuantityOriginal: l.QuantityOriginal, CostOriginal: l.CostOriginal})
	}
	symbolToISIN := map[string]string{}
	if insts, err := s.qry(r).ListInstruments(r.Context()); err == nil {
		for _, i := range insts {
			symbolToISIN[strings.ToUpper(i.Symbol)] = i.Isin
		}
	}
	for _, l := range lots {
		if l.Symbol != "" {
			symbolToISIN[strings.ToUpper(l.Symbol)] = l.ISIN
		}
	}
	res := reconcile.Compare(accountID, holdings, lots, symbolToISIN)
	res.Issues = issues
	writeJSON(w, 200, res)
}
