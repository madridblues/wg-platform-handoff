package supabase

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	AccountID string
	Subject   string
	ExpiresAt time.Time
}

type Verifier struct {
	secret []byte
}

func NewVerifier(secret string) *Verifier {
	return &Verifier{secret: []byte(secret)}
}

func (v *Verifier) Verify(tokenString string) (Claims, error) {
	if tokenString == "" {
		return Claims{}, errors.New("empty token")
	}

	if len(v.secret) == 0 {
		return Claims{}, errors.New("supabase jwt verifier is not configured")
	}

	mapClaims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(tokenString, mapClaims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %s", token.Method.Alg())
		}
		return v.secret, nil
	})
	if err != nil || !parsed.Valid {
		return Claims{}, errors.New("invalid token")
	}

	sub, _ := mapClaims["sub"].(string)
	if sub == "" {
		return Claims{}, errors.New("token missing sub claim")
	}

	accountID, _ := mapClaims["account_id"].(string)

	claims := Claims{
		AccountID: accountID,
		Subject:   sub,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if exp, ok := extractExpiry(mapClaims); ok {
		claims.ExpiresAt = exp
	}

	return claims, nil
}

func extractExpiry(claims jwt.MapClaims) (time.Time, bool) {
	value, ok := claims["exp"]
	if !ok {
		return time.Time{}, false
	}

	switch typed := value.(type) {
	case float64:
		return time.Unix(int64(typed), 0).UTC(), true
	case int64:
		return time.Unix(typed, 0).UTC(), true
	case int:
		return time.Unix(int64(typed), 0).UTC(), true
	}

	return time.Time{}, false
}
