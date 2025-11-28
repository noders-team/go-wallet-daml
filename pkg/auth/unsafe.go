package auth

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/noders-team/go-wallet-daml/pkg/model"
	"github.com/rs/zerolog"
)

type UnsafeAuthController struct {
	userID       string
	adminID      string
	audience     string
	unsafeSecret string
	logger       zerolog.Logger
	accessTokens sync.Map
}

type UnsafeOption func(*UnsafeAuthController)

func UnsafeWithUserID(userID string) UnsafeOption {
	return func(c *UnsafeAuthController) {
		c.userID = userID
	}
}

func UnsafeWithAdminID(adminID string) UnsafeOption {
	return func(c *UnsafeAuthController) {
		c.adminID = adminID
	}
}

func UnsafeWithAudience(audience string) UnsafeOption {
	return func(c *UnsafeAuthController) {
		c.audience = audience
	}
}

func UnsafeWithSecret(secret string) UnsafeOption {
	return func(c *UnsafeAuthController) {
		c.unsafeSecret = secret
	}
}

func UnsafeWithLogger(logger zerolog.Logger) UnsafeOption {
	return func(c *UnsafeAuthController) {
		c.logger = logger
	}
}

func NewUnsafeAuthController(opts ...UnsafeOption) *UnsafeAuthController {
	logger := zerolog.Nop()
	c := &UnsafeAuthController{
		logger: logger,
	}

	for _, opt := range opts {
		opt(c)
	}

	c.logger = c.logger.With().Str("component", "unsafe-auth-controller").Logger()

	return c
}

func (u *UnsafeAuthController) UserID() string {
	return u.userID
}

func (u *UnsafeAuthController) GetUserToken(ctx context.Context) (*model.AuthContext, error) {
	return u.getOrCreateJWTToken(u.userID, "user")
}

func (u *UnsafeAuthController) GetAdminToken(ctx context.Context) (*model.AuthContext, error) {
	adminID := u.adminID
	if adminID == "" {
		adminID = "admin"
	}
	return u.getOrCreateJWTToken(adminID, "admin")
}

func (u *UnsafeAuthController) getOrCreateJWTToken(sub string, subIdentifier string) (*model.AuthContext, error) {
	if u.unsafeSecret == "" {
		return nil, fmt.Errorf("unsafeSecret is not set")
	}

	if token, ok := u.accessTokens.Load(subIdentifier); ok {
		if tokenStr, ok := token.(string); ok && u.isJWTValid(tokenStr) {
			u.logger.Debug().Msg("Using cached token")
			return &model.AuthContext{
				UserID:      sub,
				AccessToken: tokenStr,
			}, nil
		}
	}

	u.logger.Debug().Msg("Creating new token")

	now := time.Now()
	claims := jwt.MapClaims{
		"sub": sub,
		"aud": u.audience,
		"iat": now.Unix(),
		"exp": now.Add(1 * time.Hour).Unix(),
		"iss": "unsafe-auth",
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(u.unsafeSecret))
	if err != nil {
		return nil, fmt.Errorf("failed to sign JWT token: %w", err)
	}

	u.accessTokens.Store(subIdentifier, tokenString)

	return &model.AuthContext{
		UserID:      sub,
		AccessToken: tokenString,
	}, nil
}

func (u *UnsafeAuthController) isJWTValid(tokenStr string) bool {
	token, _, err := jwt.NewParser().ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil {
		return false
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return false
	}

	exp, ok := claims["exp"].(float64)
	if !ok {
		return false
	}

	return time.Now().Unix() < int64(exp)
}
