package sdk

import (
	"context"
	"fmt"

	"github.com/noders-team/go-daml/pkg/client"
	"github.com/noders-team/go-wallet-daml/pkg/auth"
	proxyClient "github.com/noders-team/go-wallet-daml/pkg/client"
	"github.com/noders-team/go-wallet-daml/pkg/controller"
	"github.com/noders-team/go-wallet-daml/pkg/model"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type (
	AuthFactory          func() auth.AuthController
	LedgerFactory        func(userID string, provider *auth.AuthTokenProvider, isAdmin bool) (*controller.LedgerController, error)
	TokenStandardFactory func(userID string, damlClient *client.DamlBindingClient) (*controller.TokenStandardController, error)
	ValidatorFactory     func(userID string, scanProxy *proxyClient.ScanProxyClient, damlClient *client.DamlBindingClient) (*controller.ValidatorController, error)
)

type Config struct {
	AuthFactory          AuthFactory
	LedgerFactory        LedgerFactory
	TokenStandardFactory TokenStandardFactory
	ValidatorFactory     ValidatorFactory
	DamlClient           *client.DamlBindingClient
	ScanProxy            *proxyClient.ScanProxyClient
}

type WalletSDK struct {
	authController    auth.AuthController
	authTokenProvider *auth.AuthTokenProvider
	userLedger        *controller.LedgerController
	adminLedger       *controller.LedgerController
	tokenStandard     *controller.TokenStandardController
	validator         *controller.ValidatorController
	damlClient        *client.DamlBindingClient
	scanProxy         *proxyClient.ScanProxyClient
	logger            zerolog.Logger

	authFactory          AuthFactory
	ledgerFactory        LedgerFactory
	tokenStandardFactory TokenStandardFactory
	validatorFactory     ValidatorFactory
}

func NewWalletSDK() *WalletSDK {
	logger := log.Logger.With().Str("component", "wallet-sdk").Logger()

	return &WalletSDK{
		logger: logger,
	}
}

func (w *WalletSDK) Configure(config Config) *WalletSDK {
	if config.AuthFactory != nil {
		w.authFactory = config.AuthFactory
		w.authController = w.authFactory()
		w.authTokenProvider = auth.NewAuthTokenProvider(w.authController)
	}
	if config.LedgerFactory != nil {
		w.ledgerFactory = config.LedgerFactory
	}
	if config.TokenStandardFactory != nil {
		w.tokenStandardFactory = config.TokenStandardFactory
	}
	if config.ValidatorFactory != nil {
		w.validatorFactory = config.ValidatorFactory
	}
	if config.DamlClient != nil {
		w.damlClient = config.DamlClient
	}
	if config.ScanProxy != nil {
		w.scanProxy = config.ScanProxy
	}
	return w
}

func (w *WalletSDK) Connect(ctx context.Context) error {
	if w.authController == nil {
		return fmt.Errorf("authController not configured")
	}

	authCtx, err := w.authController.GetUserToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get user token: %w", err)
	}

	if w.ledgerFactory != nil {
		w.userLedger, err = w.ledgerFactory(authCtx.UserID, w.authTokenProvider, false)
		if err != nil {
			return fmt.Errorf("failed to create user ledger: %w", err)
		}

		if err := w.userLedger.AwaitInit(ctx); err != nil {
			return fmt.Errorf("failed to initialize user ledger: %w", err)
		}
	}

	if w.tokenStandardFactory != nil {
		w.tokenStandard, err = w.tokenStandardFactory(authCtx.UserID, w.damlClient)
		if err != nil {
			return fmt.Errorf("failed to create token standard controller: %w", err)
		}
	}

	if w.validatorFactory != nil {
		w.validator, err = w.validatorFactory(authCtx.UserID, w.scanProxy, w.damlClient)
		if err != nil {
			return fmt.Errorf("failed to create validator controller: %w", err)
		}
	}

	return nil
}

func (w *WalletSDK) ConnectAdmin(ctx context.Context) error {
	if w.authController == nil {
		return fmt.Errorf("authController not configured")
	}

	authCtx, err := w.authController.GetAdminToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get admin token: %w", err)
	}

	if w.ledgerFactory != nil {
		w.adminLedger, err = w.ledgerFactory(authCtx.UserID, w.authTokenProvider, true)
		if err != nil {
			return fmt.Errorf("failed to create admin ledger: %w", err)
		}
	}

	return nil
}

func (w *WalletSDK) SetPartyID(ctx context.Context, partyID model.PartyID, synchronizerID *model.PartyID) error {
	var syncID model.PartyID
	if synchronizerID != nil {
		syncID = *synchronizerID
	} else {
		if w.userLedger == nil {
			return fmt.Errorf("userLedger not initialized")
		}

		syncResp, err := w.userLedger.ListSynchronizers(ctx, partyID)
		if err != nil {
			return fmt.Errorf("failed to list synchronizers: %w", err)
		}

		if len(syncResp.ConnectedSynchronizers) == 0 {
			return fmt.Errorf("no synchronizers found for party %s", partyID)
		}

		syncID = model.PartyID(syncResp.ConnectedSynchronizers[0].SynchronizerID)
	}

	w.logger.Info().Msgf("synchronizer id will be set to %s", syncID)

	if w.userLedger != nil {
		w.userLedger.SetPartyID(partyID)
		w.userLedger.SetSynchronizerID(syncID)
	}

	if w.tokenStandard != nil {
		w.tokenStandard.SetPartyID(partyID)
		w.tokenStandard.SetSynchronizerID(syncID)
	}

	if w.validator != nil {
		w.validator.SetPartyID(partyID)
		w.validator.SetSynchronizerID(syncID)
	}

	return nil
}

func (w *WalletSDK) Auth() auth.AuthController {
	return w.authController
}

func (w *WalletSDK) AuthTokenProvider() *auth.AuthTokenProvider {
	return w.authTokenProvider
}

func (w *WalletSDK) UserLedger() *controller.LedgerController {
	return w.userLedger
}

func (w *WalletSDK) AdminLedger() *controller.LedgerController {
	return w.adminLedger
}

func (w *WalletSDK) TokenStandard() *controller.TokenStandardController {
	return w.tokenStandard
}

func (w *WalletSDK) Validator() *controller.ValidatorController {
	return w.validator
}
