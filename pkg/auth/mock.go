package auth

import (
	"context"

	"github.com/noders-team/go-wallet-daml/pkg/model"
)

type MockAuthController struct {
	userID string
}

func NewMockAuthController(userID string) *MockAuthController {
	return &MockAuthController{
		userID: userID,
	}
}

func (m *MockAuthController) GetUserToken(ctx context.Context) (*model.AuthContext, error) {
	return &model.AuthContext{
		UserID:      m.userID,
		AccessToken: "mock-user-token",
	}, nil
}

func (m *MockAuthController) GetAdminToken(ctx context.Context) (*model.AuthContext, error) {
	return &model.AuthContext{
		UserID:      m.userID,
		AccessToken: "mock-admin-token",
	}, nil
}

func (m *MockAuthController) UserID() string {
	return m.userID
}

func NewMockAuthTokenProvider(userID string) *AuthTokenProvider {
	mockController := NewMockAuthController(userID)
	return NewAuthTokenProvider(mockController)
}
