package auth

import (
	"context"
	"sync"
)

type AuthTokenProvider struct {
	authController AuthController
	cache          sync.Map
}

func NewAuthTokenProvider(controller AuthController) *AuthTokenProvider {
	return &AuthTokenProvider{
		authController: controller,
	}
}

func (a *AuthTokenProvider) GetUserAccessToken(ctx context.Context) (string, error) {
	authCtx, err := a.authController.GetUserToken(ctx)
	if err != nil {
		return "", err
	}
	return authCtx.AccessToken, nil
}

func (a *AuthTokenProvider) GetAdminAccessToken(ctx context.Context) (string, error) {
	authCtx, err := a.authController.GetAdminToken(ctx)
	if err != nil {
		return "", err
	}
	return authCtx.AccessToken, nil
}

func (a *AuthTokenProvider) RefreshToken(ctx context.Context, isAdmin bool) (string, error) {
	if isAdmin {
		return a.GetAdminAccessToken(ctx)
	}
	return a.GetUserAccessToken(ctx)
}
