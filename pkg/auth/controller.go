package auth

import (
	"context"

	"github.com/noders-team/go-wallet-daml/pkg/model"
)

type AuthController interface {
	GetUserToken(ctx context.Context) (*model.AuthContext, error)
	GetAdminToken(ctx context.Context) (*model.AuthContext, error)
	UserID() string
}
