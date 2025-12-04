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
	"github.com/noders-team/go-wallet-daml/pkg/wrapper"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type ValidatorController struct {
	damlClient      *client.DamlBindingClient
	scanProxyClient *wrapper.ScanProxyClient
	userID          string
	partyID         atomic.Value
	synchronizerID  atomic.Value
	logger          zerolog.Logger
}

func NewValidatorController(userID string, grpcAddress string, scanProxyBaseURL string, provider *auth.AuthTokenProvider) (*ValidatorController, error) {
	logger := log.Logger.With().
		Str("component", "validator-controller").
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

	scanProxyClient := wrapper.NewScanProxyClient(scanProxyBaseURL, provider, false)

	return &ValidatorController{
		damlClient:      client.NewDamlBindingClient(&client.DamlClient{}, conn.GRPCConn()),
		scanProxyClient: scanProxyClient,
		userID:          userID,
		logger:          logger,
	}, nil
}

func (v *ValidatorController) SetPartyID(partyID model.PartyID) {
	v.partyID.Store(partyID)
}

func (v *ValidatorController) SetSynchronizerID(synchronizerID model.PartyID) {
	v.synchronizerID.Store(synchronizerID)
}

func (v *ValidatorController) GetPartyID() (model.PartyID, error) {
	val := v.partyID.Load()
	if val == nil {
		return "", fmt.Errorf("partyID not set")
	}
	return val.(model.PartyID), nil
}

func (v *ValidatorController) GetSynchronizerID() (model.PartyID, error) {
	val := v.synchronizerID.Load()
	if val == nil {
		return "", fmt.Errorf("synchronizerID not set")
	}
	return val.(model.PartyID), nil
}

func (v *ValidatorController) GetValidatorUser(ctx context.Context) (model.PartyID, error) {
	partyID, err := v.GetPartyID()
	if err != nil {
		return "", err
	}

	filter := &damlModel.TransactionFilter{
		FiltersByParty: map[string]*damlModel.Filters{
			string(partyID): {
				Inclusive: &damlModel.InclusiveFilters{
					TemplateFilters: []*damlModel.TemplateFilter{
						{
							TemplateID:              "#splice-validator:Splice.Validator:ValidatorLicense",
							IncludeCreatedEventBlob: false,
						},
					},
				},
			},
		},
	}

	req := &damlModel.GetActiveContractsRequest{
		Filter:      filter,
		EventFormat: &damlModel.EventFormat{Verbose: true},
	}

	stream, errChan := v.damlClient.StateService.GetActiveContracts(ctx, req)

	for {
		select {
		case resp, ok := <-stream:
			if !ok {
				return "", fmt.Errorf("validator user not found")
			}
			if entry, ok := resp.ContractEntry.(*damlModel.ActiveContractEntry); ok {
				if entry.ActiveContract != nil && entry.ActiveContract.CreatedEvent != nil {
					contract := entry.ActiveContract.CreatedEvent
					if validatorParty, ok := contract.CreateArguments.(map[string]interface{})["validator"]; ok {
						if validatorPartyStr, ok := validatorParty.(string); ok {
							return model.PartyID(validatorPartyStr), nil
						}
					}
				}
			}
		case err := <-errChan:
			if err != nil {
				return "", fmt.Errorf("failed to get validator user: %w", err)
			}
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

func (v *ValidatorController) GetTransferPreApprovalByParty(ctx context.Context, receiverID model.PartyID) (*model.TransferPreapproval, error) {
	contract, err := v.scanProxyClient.GetTransferPreApprovalByParty(ctx, receiverID)
	if err != nil {
		return nil, err
	}

	if contract == nil {
		return nil, nil
	}

	preapproval := &model.TransferPreapproval{
		ReceiverID: receiverID,
	}

	if dso, ok := contract.Payload["expectedDso"].(string); ok {
		preapproval.DSO = model.PartyID(dso)
	}

	if expiresAtStr, ok := contract.Payload["expiresAt"].(string); ok {
		if expiresAt, err := time.Parse(time.RFC3339, expiresAtStr); err == nil {
			preapproval.ExpiresAt = expiresAt
		}
	}

	return preapproval, nil
}

func (v *ValidatorController) GetOpenMiningRounds(ctx context.Context) ([]*model.OpenMiningRound, error) {
	contracts, err := v.scanProxyClient.GetOpenMiningRounds(ctx)
	if err != nil {
		return nil, err
	}

	rounds := make([]*model.OpenMiningRound, 0, len(contracts))

	for _, contract := range contracts {
		round := &model.OpenMiningRound{}

		if roundID, ok := contract.Payload["round"].(map[string]interface{}); ok {
			if number, ok := roundID["number"].(string); ok {
				round.RoundID = number
			}
		}

		if opensAtStr, ok := contract.Payload["opensAt"].(string); ok {
			if opensAt, err := time.Parse(time.RFC3339, opensAtStr); err == nil {
				round.StartTime = opensAt
			}
		}

		if targetClosesAtStr, ok := contract.Payload["targetClosesAt"].(string); ok {
			if targetClosesAt, err := time.Parse(time.RFC3339, targetClosesAtStr); err == nil {
				round.EndTime = targetClosesAt
			}
		}

		rounds = append(rounds, round)
	}

	return rounds, nil
}

func (v *ValidatorController) GetAmuletRules(ctx context.Context) (*model.AmuletRules, error) {
	contract, err := v.scanProxyClient.GetAmuletRules(ctx)
	if err != nil {
		return nil, err
	}

	if contract == nil {
		return nil, fmt.Errorf("amulet rules not found")
	}

	rules := &model.AmuletRules{
		Rules: contract.Payload,
	}

	if dso, ok := contract.Payload["dso"].(string); ok {
		rules.DSOParty = model.PartyID(dso)
	}

	return rules, nil
}
