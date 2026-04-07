package main

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/noders-team/go-daml/pkg/client"
	"github.com/noders-team/go-wallet-daml/pkg/auth"
	"github.com/noders-team/go-wallet-daml/pkg/controller"
	"github.com/noders-team/go-wallet-daml/pkg/sdk"
	"github.com/noders-team/go-wallet-daml/pkg/wrapper"
)

func main() {
	ctx := context.Background()

	walletSDK := sdk.NewWalletSDK()

	authCtrl := auth.NewUnsafeAuthController(
		auth.UnsafeWithUserID("ledger-api-user"),
		auth.UnsafeWithAdminID("ledger-api-user"),
		auth.UnsafeWithAudience("https://canton.network.global"),
		auth.UnsafeWithSecret("unsafe"),
	)
	authProvider := auth.NewAuthTokenProvider(authCtrl)

	token, err := authProvider.GetUserAccessToken(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to get access token")
	}

	damlCl, err := client.NewDamlClient(token, "localhost:5003").Build(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to build DAML client")
	}

	scanProxy := wrapper.NewScanProxyClient("http://localhost:5003", authProvider, false)

	walletSDK.Configure(sdk.Config{
		AuthFactory: func() auth.AuthController {
			return authCtrl
		},
		LedgerFactory: func(userID string, provider *auth.AuthTokenProvider, isAdmin bool) (*controller.LedgerController, error) {
			return controller.NewLedgerController(
				userID,
				"localhost:5003",
				"http://localhost:5003",
				provider,
				isAdmin,
			)
		},
		TokenStandardFactory: func(userID string, dc *client.DamlBindingClient) (*controller.TokenStandardController, error) {
			return controller.NewTokenStandardController(userID, dc)
		},
		ValidatorFactory: func(userID string, sp *wrapper.ScanProxyClient, dc *client.DamlBindingClient) (*controller.ValidatorController, error) {
			return controller.NewValidatorController(userID, sp, dc)
		},
		DamlClient: damlCl,
		ScanProxy:  scanProxy,
	})

	if err := walletSDK.Connect(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to connect")
	}
}
