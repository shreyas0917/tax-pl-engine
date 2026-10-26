package httpapi

import (
	"errors"
	"github.com/go-chi/chi/v5"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/shreyas0917/taxpl/internal/auth"
	"github.com/shreyas0917/taxpl/internal/store"
)

const refreshCookie = "taxpl_refresh"

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type tokenResponse struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
	User        userView  `json:"user"`
}

type userView struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if err := decodeJSON(r, &c); err != nil {
		writeProblem(w, r, err)
		return
	}
	fields := map[string]string{}
	email, err := auth.NormaliseEmail(c.Email)
	if err != nil {
		fields["email"] = err.Error()
	}
	if err := auth.ValidatePassword(c.Password); err != nil {
		fields["password"] = err.Error()
	}
	if len(fields) > 0 {
		writeProblem(w, r, validation(fields))
		return
	}
	hash, err := auth.HashPassword(c.Password)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	u, err := s.q.CreateUser(r.Context(), store.CreateUserParams{Email: email, PasswordHash: hash})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeProblem(w, r, conflict("an account with this email already exists"))
			return
		}
		writeProblem(w, r, err)
		return
	}
	_ = s.q.InsertAudit(r.Context(), store.InsertAuditParams{UserID: &u.ID, Entity: "user", EntityID: u.ID, Action: "register"})
	s.issueSession(w, r, u, http.StatusCreated)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if err := decodeJSON(r, &c); err != nil {
		writeProblem(w, r, err)
		return
	}
	email, err := auth.NormaliseEmail(c.Email)
	if err != nil {
		writeProblem(w, r, unauthorized(auth.ErrBadCredentials.Error()))
		return
	}
	u, err := s.q.GetUserByEmail(r.Context(), email)
	if err != nil || !auth.CheckPassword(u.PasswordHash, c.Password) {
		// Constant-ish time: hash compare already ran when the user exists.
		writeProblem(w, r, unauthorized(auth.ErrBadCredentials.Error()))
		return
	}
	s.issueSession(w, r, u, http.StatusOK)
}

func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, u store.User, status int) {
	access, exp, err := s.tokens.Issue(u.ID, s.now())
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	refresh, hash, err := auth.NewRefreshToken()
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	if _, err := s.q.CreateRefreshToken(r.Context(), store.CreateRefreshTokenParams{UserID: u.ID, TokenHash: hash, ExpiresAt: s.now().Add(s.cfg.RefreshTTL)}); err != nil {
		writeProblem(w, r, err)
		return
	}
	s.setRefreshCookie(w, refresh, s.now().Add(s.cfg.RefreshTTL))
	writeJSON(w, status, tokenResponse{AccessToken: access, ExpiresAt: exp, User: userView{ID: u.ID, Email: u.Email, CreatedAt: u.CreatedAt}})
}

func (s *Server) setRefreshCookie(w http.ResponseWriter, value string, exp time.Time) {
	http.SetCookie(w, &http.Cookie{Name: refreshCookie, Value: value, Path: "/api/v1/auth", HttpOnly: true, Secure: !s.cfg.IsDev(),
		SameSite: http.SameSiteLaxMode, Expires: exp})
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(refreshCookie)
	if err != nil || c.Value == "" {
		writeProblem(w, r, unauthorized("no refresh token"))
		return
	}
	hash := auth.HashToken(c.Value)
	rt, err := s.q.GetRefreshToken(r.Context(), hash)
	if err != nil {
		writeProblem(w, r, unauthorized("refresh token invalid or expired"))
		return
	}
	u, err := s.q.GetUser(r.Context(), rt.UserID)
	if err != nil {
		writeProblem(w, r, unauthorized("user no longer exists"))
		return
	}
	// Rotate: revoke the old token, issue a new pair.
	if err := s.q.RevokeRefreshToken(r.Context(), hash); err != nil {
		writeProblem(w, r, err)
		return
	}
	s.issueSession(w, r, u, http.StatusOK)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(refreshCookie); err == nil && c.Value != "" {
		_ = s.q.RevokeRefreshToken(r.Context(), auth.HashToken(c.Value))
	}
	s.setRefreshCookie(w, "", time.Unix(0, 0))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u, err := s.q.GetUser(r.Context(), UserID(r.Context()))
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	accounts, _ := s.q.ListAccounts(r.Context(), u.ID)
	trades, _ := s.q.CountTrades(r.Context(), u.ID)
	open, _ := s.q.CountOpenReviewItems(r.Context(), u.ID)
	writeJSON(w, 200, map[string]any{"user": userView{ID: u.ID, Email: u.Email, CreatedAt: u.CreatedAt}, "accounts": len(accounts),
		"trades": trades, "open_review_items": open, "pan_on_file": u.PanHash != nil})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeProblem(w, r, err)
		return
	}
	u, err := s.q.GetUser(r.Context(), UserID(r.Context()))
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	if !auth.CheckPassword(u.PasswordHash, body.Current) {
		writeProblem(w, r, validation(map[string]string{"current_password": "incorrect"}))
		return
	}
	if err := auth.ValidatePassword(body.New); err != nil {
		writeProblem(w, r, validation(map[string]string{"new_password": err.Error()}))
		return
	}
	hash, err := auth.HashPassword(body.New)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	if err := s.q.UpdatePassword(r.Context(), store.UpdatePasswordParams{ID: u.ID, PasswordHash: hash}); err != nil {
		writeProblem(w, r, err)
		return
	}
	_ = s.q.RevokeAllRefreshTokens(r.Context(), u.ID)
	_ = s.q.InsertAudit(r.Context(), store.InsertAuditParams{UserID: &u.ID, Entity: "user", EntityID: u.ID, Action: "change_password"})
	s.setRefreshCookie(w, "", time.Unix(0, 0))
	w.WriteHeader(http.StatusNoContent)
}

// deleteMe removes everything the user owns (spec Section 13); requires the password.
func (s *Server) deleteMe(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
		Confirm  string `json:"confirm"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeProblem(w, r, err)
		return
	}
	u, err := s.q.GetUser(r.Context(), UserID(r.Context()))
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	if !auth.CheckPassword(u.PasswordHash, body.Password) || strings.ToUpper(strings.TrimSpace(body.Confirm)) != "DELETE" {
		writeProblem(w, r, validation(map[string]string{"password": "password incorrect or confirmation missing (type DELETE)"}))
		return
	}
	if err := s.jobs.DeleteUserData(r.Context(), u.ID); err != nil {
		writeProblem(w, r, err)
		return
	}
	s.setRefreshCookie(w, "", time.Unix(0, 0))
	w.WriteHeader(http.StatusNoContent)
}

// --- accounts -----------------------------------------------------------------------------

type accountView struct {
	ID        string    `json:"id"`
	Broker    string    `json:"broker"`
	Nickname  string    `json:"nickname"`
	CreatedAt time.Time `json:"created_at"`
}

func accountViewOf(a store.DematAccount) accountView {
	return accountView{ID: a.ID, Broker: a.Broker, Nickname: a.Nickname, CreatedAt: a.CreatedAt}
}

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListAccounts(r.Context(), UserID(r.Context()))
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	out := make([]accountView, 0, len(rows))
	for _, a := range rows {
		out = append(out, accountViewOf(a))
	}
	writeJSON(w, 200, map[string]any{"items": out})
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Broker   string `json:"broker"`
		Nickname string `json:"nickname"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeProblem(w, r, err)
		return
	}
	body.Broker = strings.ToLower(strings.TrimSpace(body.Broker))
	body.Nickname = strings.TrimSpace(body.Nickname)
	fields := map[string]string{}
	if body.Broker == "" || len(body.Broker) > 40 {
		fields["broker"] = "required (zerodha, groww, upstox, angelone or other)"
	}
	if body.Nickname == "" || len(body.Nickname) > 60 {
		fields["nickname"] = "required, at most 60 characters"
	}
	if len(fields) > 0 {
		writeProblem(w, r, validation(fields))
		return
	}
	a, err := s.q.CreateAccount(r.Context(), store.CreateAccountParams{UserID: UserID(r.Context()), Broker: body.Broker, Nickname: body.Nickname})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeProblem(w, r, conflict("an account with this broker and nickname already exists"))
			return
		}
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, 201, accountViewOf(a))
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := s.q.GetAccount(r.Context(), store.GetAccountParams{ID: id, UserID: UserID(r.Context())}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeProblem(w, r, notFound("account"))
			return
		}
		writeProblem(w, r, err)
		return
	}
	if err := s.q.DeleteAccount(r.Context(), store.DeleteAccountParams{ID: id, UserID: UserID(r.Context())}); err != nil {
		writeProblem(w, r, err)
		return
	}
	if err := s.enqueueRecompute(r, UserID(r.Context())); err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id, "job_id": r.Header.Get("X-Recompute-Job")})
}
