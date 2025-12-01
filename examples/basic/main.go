package main

import (
	"context"
	"fmt"
	"os"

	"github.com/noders-team/go-wallet-daml/pkg/auth"
	"github.com/noders-team/go-wallet-daml/pkg/controller"
	"github.com/noders-team/go-wallet-daml/pkg/sdk"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	ctx := context.Background()

	walletSDK := sdk.NewWalletSDK()

	walletSDK.Configure(sdk.Config{
		AuthFactory: func() auth.AuthController {
			return auth.NewUnsafeAuthController(
				auth.UnsafeWithUserID("ledger-api-user"),
				auth.UnsafeWithAdminID("ledger-api-user"),
				auth.UnsafeWithAudience("https://canton.network.global"),
				auth.UnsafeWithSecret("unsafe"),
			)
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
		TokenStandardFactory: func(userID string, provider *auth.AuthTokenProvider, isAdmin bool) (*controller.TokenStandardController, error) {
			return controller.NewTokenStandardController(
				userID,
				"localhost:5003",
				provider,
				isAdmin,
			)
		},
		ValidatorFactory: func(userID string, provider *auth.AuthTokenProvider) (*controller.ValidatorController, error) {
			return controller.NewValidatorController(
				userID,
				"localhost:5003",
				"http://localhost:5003",
				provider,
			)
		},
	})

	if err := walletSDK.Connect(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to connect")
	}

	partyID, err := walletSDK.UserLedger().AllocateInternalParty(ctx, "my-party")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to allocate party")
	}

	if err := walletSDK.SetPartyID(ctx, partyID, nil); err != nil {
		log.Fatal().Err(err).Msg("Failed to set party ID")
	}

	fmt.Printf("Successfully allocated party: %s\n", partyID)
}
