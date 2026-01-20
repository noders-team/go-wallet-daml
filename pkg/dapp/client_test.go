package dapp

import (
	"context"
	"testing"
	"time"

	"github.com/noders-team/go-wallet-daml/pkg/auth"
	"github.com/noders-team/go-wallet-daml/pkg/controller"
	"github.com/noders-team/go-wallet-daml/pkg/sdk"
	"github.com/noders-team/go-wallet-daml/pkg/testutil"
	"github.com/stretchr/testify/require"
)

func TestDarsAvailable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	grpcAddr := testutil.GetGrpcAddr()
	scanProxyURL := testutil.GetScanProxyBaseURL()

	walletSDK := sdk.NewWalletSDK()
	walletSDK.Configure(sdk.Config{
		AuthFactory: func() auth.AuthController {
			return auth.NewMockAuthController("app-provider")
		},
		LedgerFactory: func(userID string, provider *auth.AuthTokenProvider, isAdmin bool) (*controller.LedgerController, error) {
			return controller.NewLedgerController(
				userID,
				grpcAddr,
				scanProxyURL,
				provider,
				isAdmin,
			)
		},
		TokenStandardFactory: func(userID string, provider *auth.AuthTokenProvider, isAdmin bool) (*controller.TokenStandardController, error) {
			return controller.NewTokenStandardController(
				userID,
				grpcAddr,
				provider,
				isAdmin,
			)
		},
		ValidatorFactory: func(userID string, provider *auth.AuthTokenProvider) (*controller.ValidatorController, error) {
			return controller.NewValidatorController(
				userID,
				grpcAddr,
				scanProxyURL,
				provider,
			)
		},
	})
	require.NoError(t, walletSDK.Connect(ctx))

	dappClient := NewDappClient(walletSDK, scanProxyURL)
	require.NotNil(t, dappClient)

	dars, err := dappClient.DarsAvailable(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, dars)

	foundSpliceAmulet := false
	for _, dar := range dars {
		if dar == "splice-amulet" {
			foundSpliceAmulet = true
			break
		}
	}
	require.True(t, foundSpliceAmulet, "splice-amulet package should be available")
}
