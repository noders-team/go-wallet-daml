package controller

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/noders-team/go-daml/pkg/client"
	damlModel "github.com/noders-team/go-daml/pkg/model"
	"github.com/noders-team/go-wallet-daml/pkg/auth"
	"github.com/noders-team/go-wallet-daml/pkg/model"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"
)

type TokenStandardController struct {
	damlClient     *client.DamlBindingClient
	userID         string
	partyID        atomic.Value
	synchronizerID atomic.Value
	logger         zerolog.Logger
}

func NewTokenStandardController(userID string, grpcAddress string, provider *auth.AuthTokenProvider, isAdmin bool) (*TokenStandardController, error) {
	logger := log.Logger.With().
		Str("component", "token-standard-controller").
		Str("userID", userID).
		Logger()

	tokenProviderFunc := func() (string, error) {
		ctx := context.Background()
		return provider.GetUserAccessToken(ctx)
	}

	damlConfig := &client.Config{
		Address: grpcAddress,
		TLS:     nil,
		Auth: &client.AuthConfig{
			TokenProvider: tokenProviderFunc,
		},
	}

	damlCl := client.NewClient(damlConfig)

	ctx := context.Background()
	conn, err := damlCl.Connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to DAML ledger: %w", err)
	}

	return &TokenStandardController{
		damlClient: client.NewDamlBindingClient(&client.DamlClient{}, conn.GRPCConn()),
		userID:     userID,
		logger:     logger,
	}, nil
}

func (t *TokenStandardController) SetPartyID(partyID model.PartyID) {
	t.partyID.Store(partyID)
}

func (t *TokenStandardController) SetSynchronizerID(synchronizerID model.PartyID) {
	t.synchronizerID.Store(synchronizerID)
}

func (t *TokenStandardController) GetPartyID() (model.PartyID, error) {
	v := t.partyID.Load()
	if v == nil {
		return "", fmt.Errorf("partyID not set")
	}
	return v.(model.PartyID), nil
}

func (t *TokenStandardController) GetSynchronizerID() (model.PartyID, error) {
	v := t.synchronizerID.Load()
	if v == nil {
		return "", fmt.Errorf("synchronizerID not set")
	}
	return v.(model.PartyID), nil
}

func (t *TokenStandardController) Transfer(ctx context.Context, receiver model.PartyID, amount decimal.Decimal) (*model.TransferResponse, error) {
	partyID, err := t.GetPartyID()
	if err != nil {
		return nil, err
	}

	syncID, err := t.GetSynchronizerID()
	if err != nil {
		return nil, err
	}

	transferCmd := &damlModel.Command{
		Command: damlModel.ExerciseCommand{
			TemplateID: "#splice-amulet:Splice.Amulet:Amulet",
			Choice:     "Transfer",
			Arguments: map[string]interface{}{
				"newOwner": string(receiver),
				"amount":   amount.String(),
			},
		},
	}

	prepareReq := &damlModel.PrepareSubmissionRequest{
		UserID:         t.userID,
		CommandID:      fmt.Sprintf("transfer-%d", time.Now().UnixNano()),
		Commands:       []*damlModel.Command{transferCmd},
		ActAs:          []string{string(partyID)},
		ReadAs:         []string{},
		SynchronizerID: string(syncID),
	}

	_, err = t.damlClient.InteractiveSubmissionService.PrepareSubmission(ctx, prepareReq)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare transfer: %w", err)
	}

	t.logger.Info().
		Str("receiver", string(receiver)).
		Str("amount", amount.String()).
		Msg("Transfer prepared (signature required)")

	return &model.TransferResponse{
		SubmissionID: prepareReq.CommandID,
		Amount:       amount,
		Receiver:     receiver,
	}, nil
}

func (t *TokenStandardController) Lock(ctx context.Context, amount decimal.Decimal, expiresAt time.Time) (*model.LockResponse, error) {
	_, err := t.GetPartyID()
	if err != nil {
		return nil, err
	}

	contractID := fmt.Sprintf("lock-%d", time.Now().UnixNano())

	t.logger.Info().
		Str("amount", amount.String()).
		Time("expiresAt", expiresAt).
		Msg("Lock amulet operation")

	return &model.LockResponse{
		ContractID: contractID,
		Amount:     amount,
		ExpiresAt:  expiresAt,
	}, nil
}

func (t *TokenStandardController) Unlock(ctx context.Context, lockContractID string) error {
	_, err := t.GetPartyID()
	if err != nil {
		return err
	}

	t.logger.Info().
		Str("lockContractID", lockContractID).
		Msg("Unlock amulet operation")

	return nil
}

func (t *TokenStandardController) Burn(ctx context.Context, amount decimal.Decimal) error {
	_, err := t.GetPartyID()
	if err != nil {
		return err
	}

	t.logger.Info().
		Str("amount", amount.String()).
		Msg("Burn amulet operation")

	return nil
}

func (t *TokenStandardController) GetBalance(ctx context.Context) (decimal.Decimal, error) {
	partyID, err := t.GetPartyID()
	if err != nil {
		return decimal.Zero, err
	}

	filter := &damlModel.TransactionFilter{
		FiltersByParty: map[string]*damlModel.Filters{
			string(partyID): {
				Inclusive: &damlModel.InclusiveFilters{
					TemplateFilters: []*damlModel.TemplateFilter{
						{
							TemplateID:              "#splice-amulet:Splice.Amulet:Amulet",
							IncludeCreatedEventBlob: false,
						},
					},
				},
			},
		},
	}

	req := &damlModel.GetActiveContractsRequest{
		Filter: filter,
	}

	stream, errChan := t.damlClient.StateService.GetActiveContracts(ctx, req)

	balance := decimal.Zero

	for {
		select {
		case resp, ok := <-stream:
			if !ok {
				t.logger.Debug().
					Str("partyID", string(partyID)).
					Str("balance", balance.String()).
					Msg("Balance retrieved")
				return balance, nil
			}
			for _, contract := range resp.ActiveContracts {
				if amountVal, ok := contract.CreateArguments.(map[string]interface{})["amount"]; ok {
					if amountStr, ok := amountVal.(string); ok {
						amount, err := decimal.NewFromString(amountStr)
						if err == nil {
							balance = balance.Add(amount)
						}
					}
				}
			}
		case err := <-errChan:
			if err != nil {
				return decimal.Zero, fmt.Errorf("failed to get balance: %w", err)
			}
		case <-ctx.Done():
			return decimal.Zero, ctx.Err()
		}
	}
}
