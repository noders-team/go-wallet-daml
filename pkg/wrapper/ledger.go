package wrapper

import (
	"context"
	"fmt"

	"github.com/noders-team/go-wallet-daml/pkg/auth"
	"github.com/noders-team/go-wallet-daml/pkg/model"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type LedgerWrapper struct {
	httpClient *HTTPClient
	logger     zerolog.Logger
}

func NewLedgerWrapper(httpBaseURL string, provider *auth.AuthTokenProvider) *LedgerWrapper {
	logger := log.Logger.With().Str("component", "ledger-wrapper").Logger()

	return &LedgerWrapper{
		httpClient: NewHTTPClient(httpBaseURL, provider, logger),
		logger:     logger,
	}
}

func (w *LedgerWrapper) GenerateExternalPartyTopology(
	ctx context.Context,
	synchronizerID string,
	publicKey string,
	partyHint string,
	localParticipantObservationOnly bool,
	confirmationThreshold int32,
	confirmingParticipantUIDs []string,
	observingParticipantUIDs []string,
) (*model.GenerateTransactionResponse, error) {
	reqBody := map[string]interface{}{
		"synchronizer": synchronizerID,
		"partyHint":    partyHint,
		"publicKey": map[string]interface{}{
			"format":  "CRYPTO_KEY_FORMAT_RAW",
			"keyData": publicKey,
			"keySpec": "SIGNING_KEY_SPEC_EC_CURVE25519",
		},
		"localParticipantObservationOnly": localParticipantObservationOnly,
		"confirmationThreshold":           confirmationThreshold,
		"otherConfirmingParticipantUids":  confirmingParticipantUIDs,
		"observingParticipantUids":        observingParticipantUIDs,
	}

	w.logger.Debug().Interface("request", reqBody).Msg("GenerateExternalPartyTopology request")

	var resp model.GenerateTransactionResponse
	err := w.httpClient.Post(ctx, "/v2/parties/external/generate-topology", reqBody, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to generate external party topology: %w", err)
	}

	return &resp, nil
}

func (w *LedgerWrapper) AllocateExternalParty(
	ctx context.Context,
	synchronizerID string,
	transactions []*model.TopologyTransaction,
	signatures []*model.Signature,
) (string, error) {
	reqBody := map[string]interface{}{
		"synchronizer":           synchronizerID,
		"identityProviderId":     "",
		"onboardingTransactions": transactions,
		"multiHashSignatures":    signatures,
	}

	var resp model.AllocateExternalPartyResponse
	err := w.httpClient.Post(ctx, "/v2/parties/external/allocate", reqBody, &resp)
	if err != nil {
		return "", fmt.Errorf("failed to allocate external party: %w", err)
	}

	return resp.PartyID, nil
}
