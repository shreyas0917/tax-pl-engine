// Package auth implements password hashing, JWT access tokens and opaque refresh tokens.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// Errors.
var (
	ErrInvalidToken   = errors.New("invalid or expired token")
	ErrWeakPassword   = errors.New("password must be at least 10 characters and contain a letter and a digit")
	ErrInvalidEmail   = errors.New("invalid email address")
	ErrBadCredentials = errors.New("email or password is incorrect")
)

// HashPassword uses bcrypt at a cost suited to interactive logins.
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	return string(b), err
}

// CheckPassword compares a password with its hash.
func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// ValidatePassword enforces a minimal strength rule.
func ValidatePassword(pw string) error {
	if len(pw) < 10 || len(pw) > 200 {
		return ErrWeakPassword
	}
	var letter, digit bool
	for _, r := range pw {
		switch {
		case unicode.IsLetter(r):
			letter = true
		case unicode.IsDigit(r):
			digit = true
		}
	}
	if !letter || !digit {
		return ErrWeakPassword
	}
	return nil
}

// NormaliseEmail lower-cases and validates an email.
func NormaliseEmail(e string) (string, error) {
	e = strings.ToLower(strings.TrimSpace(e))
	if _, err := mail.ParseAddress(e); err != nil || len(e) > 254 {
		return "", ErrInvalidEmail
	}
	return e, nil
}

// Tokens issues and verifies JWTs.
type Tokens struct {
	secret []byte
	ttl    time.Duration
	issuer string
}

// NewTokens builds a signer.
func NewTokens(secret string, ttl time.Duration) *Tokens {
	return &Tokens{secret: []byte(secret), ttl: ttl, issuer: "taxpl"}
}

// Claims carried in the access token.
type Claims struct {
	jwt.RegisteredClaims
}

// Issue signs an access token for a user ID.
func (t *Tokens) Issue(userID string, now time.Time) (string, time.Time, error) {
	exp := now.Add(t.ttl)
	c := Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: userID, Issuer: t.issuer, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(exp)}}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(t.secret)
	return s, exp, err
}

// Verify parses an access token and returns the user ID.
func (t *Tokens) Verify(token string) (string, error) {
	var c Claims
	parsed, err := jwt.ParseWithClaims(token, &c, func(tok *jwt.Token) (any, error) {
		if _, ok := tok.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return t.secret, nil
	}, jwt.WithIssuer(t.issuer), jwt.WithExpirationRequired())
	if err != nil || !parsed.Valid || c.Subject == "" {
		return "", ErrInvalidToken
	}
	return c.Subject, nil
}

// NewRefreshToken returns an opaque token and the hash to store.
func NewRefreshToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token), nil
}

// HashToken hashes an opaque token for storage.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// HashPAN hashes a PAN with the server secret so it is never stored in clear.
func HashPAN(pan, secret string) string {
	h := sha256.Sum256([]byte(secret + ":" + strings.ToUpper(strings.TrimSpace(pan))))
	return hex.EncodeToString(h[:])
}
