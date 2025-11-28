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
)

type ValidatorController struct {
	damlClient     *client.DamlBindingClient
	userID         string
	partyID        atomic.Value
	synchronizerID atomic.Value
	logger         zerolog.Logger
}

func NewValidatorController(userID string, grpcAddress string, provider *auth.AuthTokenProvider) (*ValidatorController, error) {
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

	return &ValidatorController{
		damlClient: client.NewDamlBindingClient(&client.DamlClient{}, conn.GRPCConn()),
		userID:     userID,
		logger:     logger,
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
		Filter: filter,
	}

	stream, errChan := v.damlClient.StateService.GetActiveContracts(ctx, req)

	for {
		select {
		case resp, ok := <-stream:
			if !ok {
				return "", fmt.Errorf("validator user not found")
			}
			for _, contract := range resp.ActiveContracts {
				if validatorParty, ok := contract.CreateArguments.(map[string]interface{})["validator"]; ok {
					if validatorPartyStr, ok := validatorParty.(string); ok {
						return model.PartyID(validatorPartyStr), nil
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
	partyID, err := v.GetPartyID()
	if err != nil {
		return nil, err
	}

	filter := &damlModel.TransactionFilter{
		FiltersByParty: map[string]*damlModel.Filters{
			string(partyID): {
				Inclusive: &damlModel.InclusiveFilters{
					TemplateFilters: []*damlModel.TemplateFilter{
						{
							TemplateID:              "#splice-wallet:Splice.Wallet.TransferPreapproval:TransferPreapproval",
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

	stream, errChan := v.damlClient.StateService.GetActiveContracts(ctx, req)

	for {
		select {
		case resp, ok := <-stream:
			if !ok {
				return nil, fmt.Errorf("transfer preapproval not found for receiver %s", receiverID)
			}
			for _, contract := range resp.ActiveContracts {
				args, ok := contract.CreateArguments.(map[string]interface{})
				if !ok {
					continue
				}
				if receiver, ok := args["receiver"].(string); ok && receiver == string(receiverID) {
					preapproval := &model.TransferPreapproval{
						ReceiverID: receiverID,
					}
					if dso, ok := args["expectedDso"].(string); ok {
						preapproval.DSO = model.PartyID(dso)
					}
					return preapproval, nil
				}
			}
		case err := <-errChan:
			if err != nil {
				return nil, fmt.Errorf("failed to get transfer preapproval: %w", err)
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (v *ValidatorController) GetOpenMiningRounds(ctx context.Context) ([]*model.OpenMiningRound, error) {
	partyID, err := v.GetPartyID()
	if err != nil {
		return nil, err
	}

	filter := &damlModel.TransactionFilter{
		FiltersByParty: map[string]*damlModel.Filters{
			string(partyID): {
				Inclusive: &damlModel.InclusiveFilters{
					TemplateFilters: []*damlModel.TemplateFilter{
						{
							TemplateID:              "#splice-amulet:Splice.Round:OpenMiningRound",
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

	stream, errChan := v.damlClient.StateService.GetActiveContracts(ctx, req)

	var rounds []*model.OpenMiningRound

	for {
		select {
		case resp, ok := <-stream:
			if !ok {
				return rounds, nil
			}
			for _, contract := range resp.ActiveContracts {
				args, ok := contract.CreateArguments.(map[string]interface{})
				if !ok {
					continue
				}
				round := &model.OpenMiningRound{}
				if roundID, ok := args["roundId"].(string); ok {
					round.RoundID = roundID
				}
				if startTime, ok := args["startTime"].(time.Time); ok {
					round.StartTime = startTime
				}
				if endTime, ok := args["endTime"].(time.Time); ok {
					round.EndTime = endTime
				}
				rounds = append(rounds, round)
			}
		case err := <-errChan:
			if err != nil {
				return nil, fmt.Errorf("failed to get open mining rounds: %w", err)
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (v *ValidatorController) GetAmuletRules(ctx context.Context) (*model.AmuletRules, error) {
	partyID, err := v.GetPartyID()
	if err != nil {
		return nil, err
	}

	filter := &damlModel.TransactionFilter{
		FiltersByParty: map[string]*damlModel.Filters{
			string(partyID): {
				Inclusive: &damlModel.InclusiveFilters{
					TemplateFilters: []*damlModel.TemplateFilter{
						{
							TemplateID:              "#splice-amulet:Splice.AmuletRules:AmuletRules",
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

	stream, errChan := v.damlClient.StateService.GetActiveContracts(ctx, req)

	for {
		select {
		case resp, ok := <-stream:
			if !ok {
				return nil, fmt.Errorf("amulet rules not found")
			}
			for _, contract := range resp.ActiveContracts {
				args, ok := contract.CreateArguments.(map[string]interface{})
				if !ok {
					continue
				}
				rules := &model.AmuletRules{
					Rules: args,
				}
				if dso, ok := args["dso"].(string); ok {
					rules.DSOParty = model.PartyID(dso)
				}
				return rules, nil
			}
		case err := <-errChan:
			if err != nil {
				return nil, fmt.Errorf("failed to get amulet rules: %w", err)
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
