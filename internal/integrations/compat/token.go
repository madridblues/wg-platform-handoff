package compat

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	AccountNumber string
	ExpiresAt     time.Time
}

type tokenClaims struct {
	AccountNumber string `json:"account_number"`
	jwt.RegisteredClaims
}

type Manager struct {
	secret []byte
	ttl    time.Duration
}

func NewManager(secret string, ttl time.Duration) *Manager {
	return &Manager{
		secret: []byte(strings.TrimSpace(secret)),
		ttl:    ttl,
	}
}

func (m *Manager) Issue(accountNumber string) (string, time.Time, error) {
	if len(m.secret) == 0 {
		return "", time.Time{}, errors.New("compat token secret is not configured")
	}

	accountNumber = strings.TrimSpace(accountNumber)
	if accountNumber == "" {
		return "", time.Time{}, errors.New("account number is required")
	}

	ttl := m.ttl
	if ttl <= 0 {
		ttl = 1 * time.Hour
	}

	expiresAt := time.Now().UTC().Add(ttl)
	claims := tokenClaims{
		AccountNumber: accountNumber,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   accountNumber,
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign compat token: %w", err)
	}

	return signed, expiresAt, nil
}

func (m *Manager) Verify(tokenString string) (Claims, error) {
	if len(m.secret) == 0 {
		return Claims{}, errors.New("compat token secret is not configured")
	}

	claims := tokenClaims{}
	parsed, err := jwt.ParseWithClaims(tokenString, &claims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %s", token.Method.Alg())
		}
		return m.secret, nil
	})
	if err != nil || !parsed.Valid {
		return Claims{}, errors.New("invalid compat token")
	}

	accountNumber := strings.TrimSpace(claims.AccountNumber)
	if accountNumber == "" {
		accountNumber = strings.TrimSpace(claims.Subject)
	}
	if accountNumber == "" {
		return Claims{}, errors.New("compat token missing account number")
	}

	out := Claims{AccountNumber: accountNumber}
	if claims.ExpiresAt != nil {
		out.ExpiresAt = claims.ExpiresAt.Time.UTC()
	}

	return out, nil
}
