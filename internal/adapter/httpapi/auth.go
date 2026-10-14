package httpapi

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/golang-jwt/jwt/v5"
)

type partyKey struct{}

// authenticate reads a bearer token if present; operations that need a party
// call requireParty.
func (s *server) authenticate(ctx huma.Context, next func(huma.Context)) {
	h := ctx.Header("Authorization")
	if tok, ok := strings.CutPrefix(h, "Bearer "); ok {
		if id, err := s.verify(tok); err == nil {
			ctx = huma.WithValue(ctx, partyKey{}, id)
		}
	}
	next(ctx)
}

func (s *server) issue(partyID string, ttl time.Duration) (string, error) {
	now := time.Now()
	return jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject: partyID, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(ttl)), Issuer: "intenza",
	}).SignedString(s.d.JWTSecret)
}

func (s *server) verify(tok string) (string, error) {
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(tok, claims, func(*jwt.Token) (any, error) { return s.d.JWTSecret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("intenza"), jwt.WithExpirationRequired())
	if err != nil {
		return "", err
	}
	if claims.Subject == "" {
		return "", errors.New("token sem sujeito")
	}
	return claims.Subject, nil
}

func requireParty(ctx context.Context) (string, error) {
	if id, ok := ctx.Value(partyKey{}).(string); ok && id != "" {
		return id, nil
	}
	return "", huma.Error401Unauthorized("token Bearer ausente ou inválido")
}
