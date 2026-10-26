// Package httpapi exposes the REST API (spec Section 9): chi router, middleware, RFC 9457
// problem+json errors and handlers that delegate to the store and jobs packages.
package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
)

// Problem is an RFC 9457 error body.
type Problem struct {
	Type     string            `json:"type"`
	Title    string            `json:"title"`
	Status   int               `json:"status"`
	Detail   string            `json:"detail,omitempty"`
	Instance string            `json:"instance,omitempty"`
	Errors   map[string]string `json:"errors,omitempty"` // field -> message
}

// APIError is an error that maps to a problem response.
type APIError struct {
	Status int
	Title  string
	Detail string
	Fields map[string]string
}

func (e *APIError) Error() string { return e.Title + ": " + e.Detail }

// Common constructors.
func badRequest(detail string) *APIError {
	return &APIError{Status: 400, Title: "Bad request", Detail: detail}
}
func unauthorized(detail string) *APIError {
	return &APIError{Status: 401, Title: "Unauthorized", Detail: detail}
}
func notFound(what string) *APIError {
	return &APIError{Status: 404, Title: "Not found", Detail: what + " not found"}
}
func conflict(detail string) *APIError {
	return &APIError{Status: 409, Title: "Conflict", Detail: detail}
}
func validation(fields map[string]string) *APIError {
	return &APIError{Status: 422, Title: "Validation failed", Detail: "One or more fields are invalid.", Fields: fields}
}
func tooLarge(detail string) *APIError {
	return &APIError{Status: 413, Title: "Payload too large", Detail: detail}
}

// writeProblem maps any error to problem+json in one place (spec Section 11).
func writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *APIError
	switch {
	case errors.As(err, &apiErr):
	case errors.Is(err, pgx.ErrNoRows):
		apiErr = notFound("resource")
	default:
		slog.ErrorContext(r.Context(), "internal error", "err", err, "request_id", middleware.GetReqID(r.Context()))
		apiErr = &APIError{Status: 500, Title: "Internal error", Detail: "Something went wrong on our side. The request ID is " + middleware.GetReqID(r.Context()) + "."}
	}
	p := Problem{Type: "about:blank", Title: apiErr.Title, Status: apiErr.Status, Detail: apiErr.Detail, Instance: r.URL.Path, Errors: apiErr.Fields}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(apiErr.Status)
	_ = json.NewEncoder(w).Encode(p)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return badRequest("invalid JSON body: " + err.Error())
	}
	return nil
}
