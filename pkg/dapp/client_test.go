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
	"github.com/stretchr/testify/suite"
)

type DappClientTestSuite struct {
	suite.Suite
	ctx          context.Context
	cancel       context.CancelFunc
	walletSDK    *sdk.WalletSDK
	dappClient   *DappClient
	grpcAddr     string
	scanProxyURL string
}

func (s *DappClientTestSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)

	s.grpcAddr = testutil.GetGrpcAddr()
	s.scanProxyURL = testutil.GetScanProxyBaseURL()

	s.walletSDK = sdk.NewWalletSDK()
	s.walletSDK.Configure(sdk.Config{
		AuthFactory: func() auth.AuthController {
			return auth.NewMockAuthController("app-provider")
		},
		LedgerFactory: func(userID string, provider *auth.AuthTokenProvider, isAdmin bool) (*controller.LedgerController, error) {
			return controller.NewLedgerController(
				userID,
				s.grpcAddr,
				s.scanProxyURL,
				provider,
				isAdmin,
			)
		},
		TokenStandardFactory: func(userID string, provider *auth.AuthTokenProvider, isAdmin bool) (*controller.TokenStandardController, error) {
			return controller.NewTokenStandardController(
				userID,
				s.grpcAddr,
				provider,
				isAdmin,
			)
		},
		ValidatorFactory: func(userID string, provider *auth.AuthTokenProvider) (*controller.ValidatorController, error) {
			return controller.NewValidatorController(
				userID,
				s.grpcAddr,
				s.scanProxyURL,
				provider,
			)
		},
	})

	err := s.walletSDK.Connect(s.ctx)
	require.NoError(s.T(), err)

	s.dappClient = NewDappClient(s.walletSDK, s.scanProxyURL)
	require.NotNil(s.T(), s.dappClient)
}

func (s *DappClientTestSuite) TearDownSuite() {
	if s.cancel != nil {
		s.cancel()
	}
}

func TestDappClientTestSuite(t *testing.T) {
	suite.Run(t, new(DappClientTestSuite))
}

func (s *DappClientTestSuite) TestDarsAvailable() {
	dars, err := s.dappClient.DarsAvailable(s.ctx)
	require.NoError(s.T(), err)
	require.NotEmpty(s.T(), dars)

	foundSpliceAmulet := false
	for _, dar := range dars {
		if dar == "splice-amulet" {
			foundSpliceAmulet = true
			break
		}
	}
	require.True(s.T(), foundSpliceAmulet, "splice-amulet package should be available")
}

func (s *DappClientTestSuite) TestRequestAccounts() {
	wallets, err := s.dappClient.RequestAccounts(s.ctx)
	require.NoError(s.T(), err)
	require.NotEmpty(s.T(), wallets)

	require.True(s.T(), len(wallets) >= 1, "there should be at least one wallet account")
	for _, wallet := range wallets {
		require.NotEmpty(s.T(), wallet.Address, "wallet address should not be empty")
		require.NotEmpty(s.T(), wallet.NetworkID, "wallet networkID should not be empty")
		require.Equal(s.T(), "local", wallet.SigningProvider, "wallet signing provider should be 'local'")
		require.Contains(s.T(), wallet.NetworkID, "canton:", "networkID should be in canton format")
	}
}
