package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/noders-team/go-daml/pkg/client"
	damlModel "github.com/noders-team/go-daml/pkg/model"
	"github.com/noders-team/go-daml/pkg/types"
	"github.com/noders-team/go-wallet-daml/pkg/model"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"
)

const (
	ALLOCATION_FACTORY_INTERFACE_ID          = "#splice-api-token-allocation-instruction-v1:Splice.Api.Token.AllocationInstructionV1:AllocationFactory"
	ALLOCATION_INSTRUCTION_INTERFACE_ID      = "#splice-api-token-allocation-instruction-v1:Splice.Api.Token.AllocationInstructionV1:AllocationInstruction"
	ALLOCATION_REQUEST_INTERFACE_ID          = "#splice-api-token-allocation-request-v1:Splice.Api.Token.AllocationRequestV1:AllocationRequest"
	ALLOCATION_INTERFACE_ID                  = "#splice-api-token-allocation-v1:Splice.Api.Token.AllocationV1:Allocation"
	HOLDING_INTERFACE_ID                     = "#splice-api-token-holding-v1:Splice.Api.Token.HoldingV1:Holding"
	METADATA_INTERFACE_ID                    = "#splice-api-token-metadata-v1:Splice.Api.Token.MetadataV1:AnyContract"
	TRANSFER_FACTORY_INTERFACE_ID            = "#splice-api-token-transfer-instruction-v1:Splice.Api.Token.TransferInstructionV1:TransferFactory"
	TRANSFER_INSTRUCTION_INTERFACE_ID        = "#splice-api-token-transfer-instruction-v1:Splice.Api.Token.TransferInstructionV1:TransferInstruction"
	FEATURED_APP_DELEGATE_PROXY_INTERFACE_ID = "#splice-util-featured-app-proxies:Splice.Util.FeaturedApp.DelegateProxy:DelegateProxy"
	MERGE_DELEGATION_PROPOSAL_TEMPLATE_ID    = "#splice-util-token-standard-wallet:Splice.Util.Token.Wallet.MergeDelegation:MergeDelegationProposal"
	MERGE_DELEGATION_TEMPLATE_ID             = "#splice-util-token-standard-wallet:Splice.Util.Token.Wallet.MergeDelegation:MergeDelegation"
	MERGE_DELEGATION_BATCH_MERGE_UTILITY     = "#splice-util-token-standard-wallet:Splice.Util.Token.Wallet.MergeDelegation:BatchMergeUtility"
)

type TokenStandardController struct {
	damlClient                 *client.DamlBindingClient
	userID                     string
	partyID                    atomic.Value
	synchronizerID             atomic.Value
	transferFactoryRegistryUrl atomic.Value
	amuletRulesContractID      atomic.Value
	amuletRulesTemplateID      atomic.Value
	openMiningRoundContractID  atomic.Value
	logger                     zerolog.Logger
}

func NewTokenStandardController(userID string, damlClient *client.DamlBindingClient) (*TokenStandardController, error) {
	logger := log.Logger.With().
		Str("component", "token-standard-controller").
		Str("userID", userID).
		Logger()

	return &TokenStandardController{
		damlClient: damlClient,
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

func (t *TokenStandardController) SetTransferFactoryRegistryUrl(url string) {
	t.transferFactoryRegistryUrl.Store(url)
}

func (t *TokenStandardController) GetTransferFactoryRegistryUrl() (string, error) {
	v := t.transferFactoryRegistryUrl.Load()
	if v == nil {
		return "", fmt.Errorf("transferFactoryRegistryUrl not set")
	}
	return v.(string), nil
}

func (t *TokenStandardController) SetAmuletRulesContractID(id string) {
	t.amuletRulesContractID.Store(id)
}

func (t *TokenStandardController) GetAmuletRulesContractID() string {
	v := t.amuletRulesContractID.Load()
	if v == nil {
		return ""
	}
	return v.(string)
}

func (t *TokenStandardController) SetAmuletRulesTemplateID(id string) {
	t.amuletRulesTemplateID.Store(id)
}

func (t *TokenStandardController) GetAmuletRulesTemplateID() string {
	v := t.amuletRulesTemplateID.Load()
	if v == nil {
		return ""
	}
	return v.(string)
}

func (t *TokenStandardController) SetOpenMiningRoundContractID(id string) {
	t.openMiningRoundContractID.Store(id)
}

func (t *TokenStandardController) GetOpenMiningRoundContractID() string {
	v := t.openMiningRoundContractID.Load()
	if v == nil {
		return ""
	}
	return v.(string)
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

func (t *TokenStandardController) Lock(ctx context.Context,
	amount decimal.Decimal, expiresAt time.Time,
) (*model.LockResponse, error) {
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

// TODO????
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

// TODO????
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

	filterByParty := map[string]*damlModel.Filters{
		string(partyID): {
			Inclusive: &damlModel.InclusiveFilters{
				TemplateFilters: []*damlModel.TemplateFilter{
					{
						TemplateID:              "3ca1343ab26b453d38c8adb70dca5f1ead8440c42b59b68f070786955cbf9ec1:Splice.Amulet:Amulet",
						IncludeCreatedEventBlob: false, // TODO no hardcoded values
					},
				},
			},
		},
	}

	req := &damlModel.GetActiveContractsRequest{
		EventFormat: &damlModel.EventFormat{
			Verbose:        true,
			FiltersByParty: filterByParty,
		},
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
					Msg("balance retrieved")
				return balance, nil
			}
			if entry, ok := resp.ContractEntry.(*damlModel.ActiveContractEntry); ok {
				if entry.ActiveContract != nil && entry.ActiveContract.CreatedEvent != nil {
					contract := entry.ActiveContract.CreatedEvent
					if amountVal, ok := contract.CreateArguments.(map[string]interface{})["amount"]; ok {
						if amountStr, ok := amountVal.(string); ok {
							amount, err := decimal.NewFromString(amountStr)
							if err == nil {
								balance = balance.Add(amount)
							}
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

func (t *TokenStandardController) ListContractsByInterface(ctx context.Context, interfaceID string) ([]*damlModel.CreatedEvent, error) {
	partyID, err := t.GetPartyID()
	if err != nil {
		return nil, err
	}

	filterByParty := map[string]*damlModel.Filters{
		string(partyID): {
			Inclusive: &damlModel.InclusiveFilters{
				InterfaceFilters: []*damlModel.InterfaceFilter{
					{
						InterfaceID:             interfaceID,
						IncludeCreatedEventBlob: true,
					},
				},
			},
		},
	}

	req := &damlModel.GetActiveContractsRequest{
		EventFormat: &damlModel.EventFormat{
			Verbose:        true,
			FiltersByParty: filterByParty,
		},
	}

	stream, errChan := t.damlClient.StateService.GetActiveContracts(ctx, req)

	var contracts []*damlModel.CreatedEvent

	for {
		select {
		case resp, ok := <-stream:
			if !ok {
				return contracts, nil
			}
			if entry, ok := resp.ContractEntry.(*damlModel.ActiveContractEntry); ok {
				if entry.ActiveContract != nil && entry.ActiveContract.CreatedEvent != nil {
					contracts = append(contracts, entry.ActiveContract.CreatedEvent)
				}
			}
		case err := <-errChan:
			if err != nil {
				return nil, fmt.Errorf("failed to list contracts by interface: %w", err)
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (t *TokenStandardController) ListHoldingUtxos(ctx context.Context, includeLocked bool, limit int) ([]*model.HoldingUTXO, error) {
	partyID, err := t.GetPartyID()
	if err != nil {
		return nil, err
	}

	packages, err := t.damlClient.PackageMng.ListKnownPackages(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list packages: %w", err)
	}

	var spliceAmuletPkgID string
	for _, pkg := range packages {
		if pkg.Name == "splice-amulet" {
			spliceAmuletPkgID = pkg.PackageID
			break
		}
	}

	if spliceAmuletPkgID == "" {
		return nil, fmt.Errorf("splice-amulet package not found")
	}

	amuletTemplateID := fmt.Sprintf("%s:Splice.Amulet:Amulet", spliceAmuletPkgID)

	req := &damlModel.GetUpdatesRequest{
		BeginExclusive: 0,
		UpdateFormat: &damlModel.EventFormat{
			Verbose: true,
			FiltersByParty: map[string]*damlModel.Filters{
				string(partyID): {
					Inclusive: &damlModel.InclusiveFilters{
						TemplateFilters: []*damlModel.TemplateFilter{
							{
								TemplateID:              amuletTemplateID,
								IncludeCreatedEventBlob: true,
							},
						},
					},
				},
			},
		},
	}

	stream, errChan := t.damlClient.UpdateService.GetUpdates(ctx, req)

	activeContracts := make(map[string]*model.HoldingUTXO)
	timeout := time.After(2 * time.Second)
	updateCount := 0

	for {
		select {
		case resp, ok := <-stream:
			if !ok {
				t.logger.Debug().
					Int("totalUpdates", updateCount).
					Int("activeContracts", len(activeContracts)).
					Msg("GetUpdates stream closed")
				result := make([]*model.HoldingUTXO, 0, len(activeContracts))
				for _, utxo := range activeContracts {
					result = append(result, utxo)
				}
				if limit > 0 && len(result) > limit {
					return result[:limit], nil
				}
				return result, nil
			}

			updateCount++
			if resp.Update != nil && resp.Update.Transaction != nil {
				t.logger.Debug().
					Int("eventCount", len(resp.Update.Transaction.Events)).
					Str("updateID", resp.Update.Transaction.UpdateID).
					Msg("Received transaction update")
				for _, event := range resp.Update.Transaction.Events {
					if event.Created != nil {
						contract := event.Created
						t.logger.Debug().
							Str("contractID", contract.ContractID).
							Str("templateID", contract.TemplateID).
							Msg("Found created Amulet contract")

						utxo := &model.HoldingUTXO{
							ContractID:       contract.ContractID,
							CreatedEventBlob: contract.CreatedEventBlob,
							InstrumentID:     "Amulet",
							InstrumentAdmin:  string(partyID),
						}

						jsonBytes, err := json.Marshal(contract.CreateArguments)
						if err != nil {
							t.logger.Warn().
								Err(err).
								Str("contractID", contract.ContractID).
								Msg("Failed to marshal CreateArguments to JSON")
							continue
						}

						var recordVal map[string]interface{}
						if err := json.Unmarshal(jsonBytes, &recordVal); err != nil {
							t.logger.Warn().
								Err(err).
								Str("contractID", contract.ContractID).
								Msg("Failed to unmarshal JSON to map")
							continue
						}

						if fieldsVal, ok := recordVal["fields"].([]interface{}); ok {
							for _, field := range fieldsVal {
								if fieldMap, ok := field.(map[string]interface{}); ok {
									label, _ := fieldMap["label"].(string)
									value := fieldMap["value"]

									if label == "amount" {
										if valueMap, ok := value.(map[string]interface{}); ok {
											if sumMap, ok := valueMap["Sum"].(map[string]interface{}); ok {
												if recordMap, ok := sumMap["Record"].(map[string]interface{}); ok {
													if recordFields, ok := recordMap["fields"].([]interface{}); ok {
														for _, rf := range recordFields {
															if rfMap, ok := rf.(map[string]interface{}); ok {
																if rfMap["label"] == "initialAmount" {
																	if rfValue, ok := rfMap["value"].(map[string]interface{}); ok {
																		if rfSum, ok := rfValue["Sum"].(map[string]interface{}); ok {
																			if amountStr, ok := rfSum["Numeric"].(string); ok {
																				utxo.Amount, _ = decimal.NewFromString(amountStr)
																			}
																		}
																	}
																}
															}
														}
													}
												} else if numericStr, ok := sumMap["Numeric"].(string); ok {
													utxo.Amount, _ = decimal.NewFromString(numericStr)
												}
											}
										}
									} else if label == "owner" {
										if valueMap, ok := value.(map[string]interface{}); ok {
											if sumMap, ok := valueMap["Sum"].(map[string]interface{}); ok {
												if partyStr, ok := sumMap["Party"].(string); ok {
													utxo.Owner = partyStr
												}
											}
										}
									}
								}
							}
						}

						activeContracts[contract.ContractID] = utxo
						t.logger.Debug().
							Str("contractID", contract.ContractID).
							Str("amount", utxo.Amount.String()).
							Int("totalActive", len(activeContracts)).
							Msg("Added contract to active set")
					} else if event.Archived != nil {
						delete(activeContracts, event.Archived.ContractID)
						t.logger.Debug().
							Str("contractID", event.Archived.ContractID).
							Msg("Removed archived contract")
					}
				}
			}
		case err := <-errChan:
			if err != nil {
				return nil, fmt.Errorf("failed to list holding utxos: %w", err)
			}
		case <-timeout:
			t.logger.Debug().
				Int("activeContracts", len(activeContracts)).
				Int("totalUpdates", updateCount).
				Msg("Timeout reached, returning active contracts")
			result := make([]*model.HoldingUTXO, 0, len(activeContracts))
			for _, utxo := range activeContracts {
				result = append(result, utxo)
			}
			if limit > 0 && len(result) > limit {
				return result[:limit], nil
			}
			return result, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (t *TokenStandardController) FetchPendingTransferInstructionView(ctx context.Context) ([]*model.TransferInstruction, error) {
	contracts, err := t.ListContractsByInterface(ctx, "Splice.TransferInstruction:TransferInstruction")
	if err != nil {
		return nil, err
	}

	var instructions []*model.TransferInstruction
	for _, contract := range contracts {
		args, ok := contract.CreateArguments.(map[string]interface{})
		if !ok {
			continue
		}

		instruction := &model.TransferInstruction{
			ContractID:       contract.ContractID,
			CreatedEventBlob: contract.CreatedEventBlob,
		}

		if sender, ok := args["sender"].(string); ok {
			instruction.Sender = sender
		}
		if receiver, ok := args["receiver"].(string); ok {
			instruction.Receiver = receiver
		}
		if amountVal, ok := args["amount"]; ok {
			if amountStr, ok := amountVal.(string); ok {
				instruction.Amount, _ = decimal.NewFromString(amountStr)
			}
		}
		if memo, ok := args["memo"].(string); ok {
			instruction.Memo = memo
		}

		instructions = append(instructions, instruction)
	}

	return instructions, nil
}

type CreateTransferResult struct {
	Command            *damlModel.Command
	DisclosedContracts []*damlModel.DisclosedContract
}

type InputAmuletVariant struct {
	ContractID string
}

func (v InputAmuletVariant) GetVariantTag() string {
	return "InputAmulet"
}

func (v InputAmuletVariant) GetVariantValue() interface{} {
	return types.CONTRACT_ID(v.ContractID)
}

func decimalToNumeric(d decimal.Decimal) types.NUMERIC {
	scaled := d.Mul(decimal.NewFromInt(10000000000))
	return types.NUMERIC(scaled.BigInt())
}

func (t *TokenStandardController) CreateTransfer(
	ctx context.Context,
	sender model.PartyID,
	receiver model.PartyID,
	amount decimal.Decimal,
	instrumentID string,
	instrumentAdmin string,
	inputUtxos []string,
	memo string,
) (*CreateTransferResult, error) {
	if instrumentAdmin == "" {
		return nil, fmt.Errorf("instrumentAdmin is required")
	}

	amuletRulesTemplateID, amuletRulesContractID, err := t.findAmuletRulesContract(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to find AmuletRules contract: %w", err)
	}

	t.logger.Debug().
		Str("amuletRulesTemplateID", amuletRulesTemplateID).
		Str("amuletRulesContractID", amuletRulesContractID).
		Msg("Found AmuletRules contract for transfer")

	if amuletRulesContractID == "" {
		return nil, fmt.Errorf("amuletRulesContractID is empty")
	}

	openMiningRoundContractID := t.GetOpenMiningRoundContractID()
	if openMiningRoundContractID == "" {
		openMiningRoundContractID = os.Getenv("OPEN_MINING_ROUND_CONTRACT_ID")
	}
	if openMiningRoundContractID == "" {
		return nil, fmt.Errorf("openMiningRoundContractID not set")
	}

	var utxosToUse []string
	if len(inputUtxos) > 0 {
		utxosToUse = inputUtxos
	} else {
		holdings, err := t.ListHoldingUtxos(ctx, false, 100)
		if err != nil {
			return nil, fmt.Errorf("failed to list holding utxos: %w", err)
		}

		var totalAmount decimal.Decimal
		for _, holding := range holdings {
			if holding.InstrumentID == instrumentID && holding.InstrumentAdmin == instrumentAdmin {
				utxosToUse = append(utxosToUse, holding.ContractID)
				totalAmount = totalAmount.Add(holding.Amount)
				if totalAmount.GreaterThanOrEqual(amount) {
					break
				}
			}
		}

		if totalAmount.LessThan(amount) {
			return nil, fmt.Errorf("insufficient balance: have %s, need %s", totalAmount.String(), amount.String())
		}
	}

	if len(utxosToUse) == 0 {
		return nil, fmt.Errorf("no utxos available for transfer")
	}

	var transferInputs []interface{}
	for _, utxo := range utxosToUse {
		transferInputs = append(transferInputs, InputAmuletVariant{ContractID: utxo})
	}

	transferOutput := map[string]interface{}{
		"receiver":         types.PARTY(string(receiver)),
		"receiverFeeRatio": decimalToNumeric(decimal.Zero),
		"amount":           decimalToNumeric(amount),
	}

	transferStruct := map[string]interface{}{
		"sender":   types.PARTY(string(sender)),
		"provider": types.PARTY(string(sender)),
		"inputs":   transferInputs,
		"outputs":  []interface{}{transferOutput},
	}

	emptyGenMap := map[string]interface{}{
		"_type": "genmap",
		"value": make(map[string]interface{}),
	}

	transferContext := map[string]interface{}{
		"openMiningRound":     types.CONTRACT_ID(openMiningRoundContractID),
		"issuingMiningRounds": emptyGenMap,
		"validatorRights":     emptyGenMap,
	}

	dsoParty := types.PARTY(instrumentAdmin)
	expectedDso := map[string]interface{}{
		"_type": "optional",
		"value": dsoParty,
	}

	transferCmd := &damlModel.Command{
		Command: &damlModel.ExerciseCommand{
			TemplateID: amuletRulesTemplateID,
			ContractID: amuletRulesContractID,
			Choice:     "AmuletRules_Transfer",
			Arguments: map[string]interface{}{
				"transfer":    transferStruct,
				"context":     transferContext,
				"expectedDso": expectedDso,
			},
		},
	}

	disclosed := make([]*damlModel.DisclosedContract, 0)

	return &CreateTransferResult{
		Command:            transferCmd,
		DisclosedContracts: disclosed,
	}, nil
}

type CreateTapResult struct {
	Command            *damlModel.Command
	DisclosedContracts []*damlModel.DisclosedContract
}

func (t *TokenStandardController) CreateTap(
	ctx context.Context,
	receiver model.PartyID,
	amount decimal.Decimal,
	instrumentAdmin string,
	instrumentID string,
) (*CreateTapResult, error) {
	amuletRulesTemplateID, amuletRulesContractID, err := t.findAmuletRulesContract(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to find AmuletRules contract: %w", err)
	}

	openMiningRoundContractID := t.GetOpenMiningRoundContractID()
	if openMiningRoundContractID == "" {
		openMiningRoundContractID = os.Getenv("OPEN_MINING_ROUND_CONTRACT_ID")
	}
	if openMiningRoundContractID == "" {
		return nil, fmt.Errorf("openMiningRoundContractID not set - OpenMiningRound needs to be bootstrapped first")
	}

	tapCmd := &damlModel.Command{
		Command: &damlModel.ExerciseCommand{
			TemplateID: amuletRulesTemplateID,
			ContractID: amuletRulesContractID,
			Choice:     "AmuletRules_DevNet_Tap",
			Arguments: map[string]interface{}{
				"receiver":  types.PARTY(string(receiver)),
				"amount":    decimalToNumeric(amount),
				"openRound": types.CONTRACT_ID(openMiningRoundContractID),
			},
		},
	}

	return &CreateTapResult{
		Command:            tapCmd,
		DisclosedContracts: []*damlModel.DisclosedContract{},
	}, nil
}

func (t *TokenStandardController) ExerciseTransferInstructionChoice(
	ctx context.Context,
	transferInstructionCid string,
	choice string,
) (*CreateTransferResult, error) {
	exerciseCmd := &damlModel.Command{
		Command: &damlModel.ExerciseCommand{
			ContractID: transferInstructionCid,
			TemplateID: "Splice.TransferInstruction:TransferInstruction",
			Choice:     choice,
			Arguments:  map[string]interface{}{},
		},
	}

	return &CreateTransferResult{
		Command:            exerciseCmd,
		DisclosedContracts: []*damlModel.DisclosedContract{},
	}, nil
}

func (t *TokenStandardController) ListHoldingTransactions(ctx context.Context, beginExclusive int64, endInclusive *int64) ([]*damlModel.GetUpdatesResponse, error) {
	partyID, err := t.GetPartyID()
	if err != nil {
		return nil, err
	}

	filter := &damlModel.TransactionFilter{
		FiltersByParty: map[string]*damlModel.Filters{
			string(partyID): {
				Inclusive: &damlModel.InclusiveFilters{
					InterfaceFilters: []*damlModel.InterfaceFilter{
						{
							InterfaceID:             "Splice.Holding:Holding", // TODO fix it
							IncludeCreatedEventBlob: true,
						},
					},
				},
			},
		},
	}

	req := &damlModel.GetUpdatesRequest{
		Filter:         filter,
		BeginExclusive: beginExclusive,
		EndInclusive:   endInclusive,
		UpdateFormat:   &damlModel.EventFormat{Verbose: true},
	}

	stream, errChan := t.damlClient.UpdateService.GetUpdates(ctx, req)

	var updates []*damlModel.GetUpdatesResponse

	for {
		select {
		case resp, ok := <-stream:
			if !ok {
				return updates, nil
			}
			updates = append(updates, resp)
		case err := <-errChan:
			if err != nil {
				return nil, fmt.Errorf("failed to list holding transactions: %w", err)
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (t *TokenStandardController) GetInstrumentAdmin(ctx context.Context) (string, error) {
	registryUrl, err := t.GetTransferFactoryRegistryUrl()
	if err != nil {
		return "", err
	}

	t.logger.Info().Str("registryUrl", registryUrl).Msg("Getting instrument admin from registry")
	return "", fmt.Errorf("registry API call not implemented - requires HTTP client")
}

func (t *TokenStandardController) GetInstrumentById(ctx context.Context, instrumentId string) (map[string]interface{}, error) {
	registryUrl, err := t.GetTransferFactoryRegistryUrl()
	if err != nil {
		return nil, err
	}

	t.logger.Info().Str("registryUrl", registryUrl).Str("instrumentId", instrumentId).Msg("Getting instrument by ID")
	return nil, fmt.Errorf("registry API call not implemented - requires HTTP client")
}

func (t *TokenStandardController) ListInstruments(ctx context.Context, pageSize int, pageToken string) (map[string]interface{}, error) {
	registryUrl, err := t.GetTransferFactoryRegistryUrl()
	if err != nil {
		return nil, err
	}

	t.logger.Info().Str("registryUrl", registryUrl).Int("pageSize", pageSize).Msg("Listing instruments")
	return nil, fmt.Errorf("registry API call not implemented - requires HTTP client")
}

func (t *TokenStandardController) GetTransactionById(ctx context.Context,
	updateId string,
) (*damlModel.GetUpdatesResponse, error) {
	partyID, err := t.GetPartyID()
	if err != nil {
		return nil, err
	}

	t.logger.Info().Str("partyID", string(partyID)).Str("updateId", updateId).Msg("Getting transaction by ID")
	return nil, fmt.Errorf("getTransactionById not fully implemented")
}

func (t *TokenStandardController) ListHoldingUtxo(ctx context.Context,
	contractId string,
) (*model.HoldingUTXO, error) {
	utxos, err := t.ListHoldingUtxos(ctx, true, 0)
	if err != nil {
		return nil, err
	}

	for _, utxo := range utxos {
		if utxo.ContractID == contractId {
			return utxo, nil
		}
	}

	return nil, fmt.Errorf("holding with contractId %s not found", contractId)
}

func (t *TokenStandardController) MergeHoldingUtxos(ctx context.Context,
	nodeLimit int, partyID model.PartyID,
	inputUtxos []*model.HoldingUTXO,
) (*model.MergeUtxosResult, error) {
	if partyID == "" {
		var err error
		partyID, err = t.GetPartyID()
		if err != nil {
			return nil, err
		}
	}

	var utxos []*model.HoldingUTXO
	if len(inputUtxos) > 0 {
		utxos = inputUtxos
	} else {
		var err error
		utxos, err = t.ListHoldingUtxos(ctx, false, nodeLimit)
		if err != nil {
			return nil, err
		}
	}

	utxosByInstrument := make(map[string][]*model.HoldingUTXO)
	for _, utxo := range utxos {
		key := utxo.InstrumentID + "::" + utxo.InstrumentAdmin
		utxosByInstrument[key] = append(utxosByInstrument[key], utxo)
	}

	var allCommands []*damlModel.Command
	var allDisclosed []*damlModel.DisclosedContract
	transferInputUtxoLimit := 100

	for _, group := range utxosByInstrument {
		if len(group) == 0 {
			continue
		}

		instrumentID := group[0].InstrumentID
		instrumentAdmin := group[0].InstrumentAdmin
		transfers := (len(group) + transferInputUtxoLimit - 1) / transferInputUtxoLimit

		for i := 0; i < transfers; i++ {
			start := i * transferInputUtxoLimit
			end := start + transferInputUtxoLimit
			if end > len(group) {
				end = len(group)
			}

			inputUtxosSlice := group[start:end]
			var totalAmount decimal.Decimal
			var inputCids []string

			for _, utxo := range inputUtxosSlice {
				totalAmount = totalAmount.Add(utxo.Amount)
				inputCids = append(inputCids, utxo.ContractID)
			}

			result, err := t.CreateTransfer(ctx, partyID, partyID, totalAmount, instrumentID, instrumentAdmin, inputCids, "merge-utxos")
			if err != nil {
				return nil, err
			}

			allCommands = append(allCommands, result.Command)
			allDisclosed = append(allDisclosed, result.DisclosedContracts...)
		}
	}

	uniqueDisclosed := make(map[string]*damlModel.DisclosedContract)
	for _, dc := range allDisclosed {
		uniqueDisclosed[dc.ContractID] = dc
	}

	disclosed := make([]*damlModel.DisclosedContract, 0, len(uniqueDisclosed))
	for _, dc := range uniqueDisclosed {
		disclosed = append(disclosed, dc)
	}

	return &model.MergeUtxosResult{
		Commands:           allCommands,
		DisclosedContracts: disclosed,
	}, nil
}

func (t *TokenStandardController) FetchPendingAllocationInstructionView(ctx context.Context) ([]*model.AllocationInstruction, error) {
	contracts, err := t.ListContractsByInterface(ctx, "Splice.Allocation:AllocationInstruction")
	if err != nil {
		return nil, err
	}

	var instructions []*model.AllocationInstruction
	for _, contract := range contracts {
		args, ok := contract.CreateArguments.(map[string]interface{})
		if !ok {
			continue
		}

		instruction := &model.AllocationInstruction{
			ContractID:       contract.ContractID,
			CreatedEventBlob: contract.CreatedEventBlob,
		}

		if provider, ok := args["provider"].(string); ok {
			instruction.Provider = provider
		}
		if spec, ok := args["specification"].(map[string]interface{}); ok {
			instruction.Specification = spec
		}

		instructions = append(instructions, instruction)
	}

	return instructions, nil
}

func (t *TokenStandardController) FetchPendingAllocationRequestView(ctx context.Context) ([]*model.AllocationRequest, error) {
	contracts, err := t.ListContractsByInterface(ctx, "Splice.Allocation:AllocationRequest")
	if err != nil {
		return nil, err
	}

	var requests []*model.AllocationRequest
	for _, contract := range contracts {
		args, ok := contract.CreateArguments.(map[string]interface{})
		if !ok {
			continue
		}

		request := &model.AllocationRequest{
			ContractID:       contract.ContractID,
			CreatedEventBlob: contract.CreatedEventBlob,
		}

		if requester, ok := args["requester"].(string); ok {
			request.Requester = requester
		}
		if spec, ok := args["specification"].(map[string]interface{}); ok {
			request.Specification = spec
		}

		requests = append(requests, request)
	}

	return requests, nil
}

func (t *TokenStandardController) FetchPendingAllocationView(ctx context.Context) ([]*model.Allocation, error) {
	contracts, err := t.ListContractsByInterface(ctx, "Splice.Allocation:Allocation")
	if err != nil {
		return nil, err
	}

	var allocations []*model.Allocation
	for _, contract := range contracts {
		args, ok := contract.CreateArguments.(map[string]interface{})
		if !ok {
			continue
		}

		allocation := &model.Allocation{
			ContractID:       contract.ContractID,
			CreatedEventBlob: contract.CreatedEventBlob,
		}

		if provider, ok := args["provider"].(string); ok {
			allocation.Provider = provider
		}
		if receiver, ok := args["receiver"].(string); ok {
			allocation.Receiver = receiver
		}
		if amountVal, ok := args["amount"]; ok {
			if amountStr, ok := amountVal.(string); ok {
				allocation.Amount, _ = decimal.NewFromString(amountStr)
			}
		}

		allocations = append(allocations, allocation)
	}

	return allocations, nil
}

func (t *TokenStandardController) CreateAllocationInstruction(
	ctx context.Context,
	allocationSpecification map[string]interface{},
	expectedAdmin string,
	inputUtxos []string,
	requestedAt string,
) (*CreateTransferResult, error) {
	cmd := &damlModel.Command{
		Command: &damlModel.ExerciseCommand{
			TemplateID: "Splice.Allocation:AllocationFactory",
			Choice:     "CreateAllocationInstruction",
			Arguments: map[string]interface{}{
				"specification": allocationSpecification,
				"expectedAdmin": expectedAdmin,
				"inputs":        inputUtxos,
				"requestedAt":   requestedAt,
			},
		},
	}

	return &CreateTransferResult{
		Command:            cmd,
		DisclosedContracts: []*damlModel.DisclosedContract{},
	}, nil
}

func (t *TokenStandardController) ExerciseAllocationChoice(
	_ context.Context,
	allocationCid string,
	choice string,
) (*CreateTransferResult, error) {
	cmd := &damlModel.Command{
		Command: &damlModel.ExerciseCommand{
			ContractID: allocationCid,
			TemplateID: "Splice.Allocation:Allocation",
			Choice:     choice,
			Arguments:  map[string]interface{}{},
		},
	}

	return &CreateTransferResult{
		Command:            cmd,
		DisclosedContracts: []*damlModel.DisclosedContract{},
	}, nil
}

func (t *TokenStandardController) ExerciseAllocationInstructionChoice(
	ctx context.Context,
	allocationInstructionCid string,
	choice string,
) (*CreateTransferResult, error) {
	cmd := &damlModel.Command{
		Command: &damlModel.ExerciseCommand{
			ContractID: allocationInstructionCid,
			TemplateID: "Splice.Allocation:AllocationInstruction",
			Choice:     choice,
			Arguments:  map[string]interface{}{},
		},
	}

	return &CreateTransferResult{
		Command:            cmd,
		DisclosedContracts: []*damlModel.DisclosedContract{},
	}, nil
}

func (t *TokenStandardController) ExerciseAllocationRequestChoice(
	ctx context.Context,
	allocationRequestCid string,
	choice string,
	actor model.PartyID,
) (*CreateTransferResult, error) {
	cmd := &damlModel.Command{
		Command: &damlModel.ExerciseCommand{
			ContractID: allocationRequestCid,
			TemplateID: "Splice.Allocation:AllocationRequest",
			Choice:     choice,
			Arguments: map[string]interface{}{
				"actor": string(actor),
			},
		},
	}

	return &CreateTransferResult{
		Command:            cmd,
		DisclosedContracts: []*damlModel.DisclosedContract{},
	}, nil
}

func (t *TokenStandardController) CreateTransferUsingDelegateProxy(
	ctx context.Context,
	proxyCid string,
	featuredAppRightCid string,
	sender model.PartyID,
	receiver model.PartyID,
	amount decimal.Decimal,
	instrumentID string,
	instrumentAdmin string,
	beneficiaries []map[string]interface{},
	inputUtxos []string,
	memo string,
) (*CreateTransferResult, error) {
	cmd := &damlModel.Command{
		Command: &damlModel.ExerciseCommand{
			ContractID: proxyCid,
			TemplateID: "Splice.DelegateProxy:DelegateProxy",
			Choice:     "CreateTransfer",
			Arguments: map[string]interface{}{
				"featuredAppRightCid": featuredAppRightCid,
				"sender":              string(sender),
				"receiver":            string(receiver),
				"amount":              amount.String(),
				"instrumentId":        instrumentID,
				"instrumentAdmin":     instrumentAdmin,
				"beneficiaries":       beneficiaries,
				"inputs":              inputUtxos,
				"memo":                memo,
			},
		},
	}

	return &CreateTransferResult{
		Command:            cmd,
		DisclosedContracts: []*damlModel.DisclosedContract{},
	}, nil
}

func (t *TokenStandardController) ExerciseTransferInstructionChoiceWithDelegate(
	ctx context.Context,
	transferInstructionCid string,
	choice string,
	proxyCid string,
	featuredAppRightCid string,
	beneficiaries []map[string]interface{},
) (*CreateTransferResult, error) {
	cmd := &damlModel.Command{
		Command: &damlModel.ExerciseCommand{
			ContractID: proxyCid,
			TemplateID: "Splice.DelegateProxy:DelegateProxy",
			Choice:     "ExerciseTransferInstructionChoice",
			Arguments: map[string]interface{}{
				"transferInstructionCid": transferInstructionCid,
				"choice":                 choice,
				"featuredAppRightCid":    featuredAppRightCid,
				"beneficiaries":          beneficiaries,
			},
		},
	}

	return &CreateTransferResult{
		Command:            cmd,
		DisclosedContracts: []*damlModel.DisclosedContract{},
	}, nil
}

type TransferPreapproval struct {
	ReceiverID model.PartyID
	ExpiresAt  time.Time
	Dso        model.PartyID
	ContractID string
	TemplateID string
}

func (t *TokenStandardController) GetTransferPreApprovalByParty(ctx context.Context, receiverID model.PartyID, instrumentID string) (*TransferPreapproval, error) {
	t.logger.Info().Str("receiverId", string(receiverID)).Str("instrumentId", instrumentID).Msg("Getting transfer preapproval by party")
	return nil, fmt.Errorf("scan proxy API call not implemented - requires HTTP client")
}

func (t *TokenStandardController) CreateCancelTransferPreapproval(
	ctx context.Context,
	contractID string,
	templateID string,
	actor model.PartyID,
) (*CreateTransferResult, error) {
	cmd := &damlModel.Command{
		Command: &damlModel.ExerciseCommand{
			ContractID: contractID,
			TemplateID: templateID,
			Choice:     "TransferPreapproval_Cancel",
			Arguments: map[string]interface{}{
				"actor": string(actor),
			},
		},
	}

	return &CreateTransferResult{
		Command:            cmd,
		DisclosedContracts: []*damlModel.DisclosedContract{},
	}, nil
}

func (t *TokenStandardController) CreateRenewTransferPreapproval(
	ctx context.Context,
	contractID string,
	templateID string,
	provider model.PartyID,
	newExpiresAt *time.Time,
	inputUtxos []string,
) (*CreateTransferResult, error) {
	syncID, err := t.GetSynchronizerID()
	if err != nil {
		return nil, err
	}

	args := map[string]interface{}{
		"provider":       string(provider),
		"synchronizerId": string(syncID),
	}

	if newExpiresAt != nil {
		args["newExpiresAt"] = newExpiresAt.Format(time.RFC3339)
	}

	if len(inputUtxos) > 0 {
		args["inputUtxos"] = inputUtxos
	}

	cmd := &damlModel.Command{
		Command: &damlModel.ExerciseCommand{
			ContractID: contractID,
			TemplateID: templateID,
			Choice:     "TransferPreapproval_Renew",
			Arguments:  args,
		},
	}

	return &CreateTransferResult{
		Command:            cmd,
		DisclosedContracts: []*damlModel.DisclosedContract{},
	}, nil
}

func (t *TokenStandardController) WaitForPreapprovalFromScanProxy(
	ctx context.Context,
	receiverID model.PartyID,
	instrumentID string,
	oldCid string,
	expectGone bool,
	intervalMs int,
	timeoutMs int,
) (*TransferPreapproval, error) {
	if intervalMs == 0 {
		intervalMs = 15000
	}
	if timeoutMs == 0 {
		timeoutMs = 300000
	}

	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	interval := time.Duration(intervalMs) * time.Millisecond

	for attempt := 1; time.Now().Before(deadline); attempt++ {
		preapproval, err := t.GetTransferPreApprovalByParty(ctx, receiverID, instrumentID)

		if expectGone {
			if preapproval == nil || err != nil {
				t.logger.Info().Int("attempt", attempt).Msg("Preapproval is no longer visible")
				return nil, nil
			}
			t.logger.Info().Int("attempt", attempt).Str("seenCid", preapproval.ContractID).Msg("Preapproval still visible - polling again")
		} else if preapproval != nil {
			if oldCid == "" {
				t.logger.Info().Int("attempt", attempt).Str("seenCid", preapproval.ContractID).Msg("Preapproval is visible")
				return preapproval, nil
			}
			if preapproval.ContractID != oldCid {
				t.logger.Info().Int("attempt", attempt).Str("oldCid", oldCid).Str("newCid", preapproval.ContractID).Msg("Preapproval CID changed")
				return preapproval, nil
			}
			t.logger.Info().Int("attempt", attempt).Str("seenCid", preapproval.ContractID).Str("oldCid", oldCid).Msg("Preapproval visible but CID unchanged - polling again")
		} else {
			t.logger.Info().Int("attempt", attempt).Msg("Preapproval not visible yet - polling again")
		}

		select {
		case <-time.After(interval):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	waitingFor := "for preapproval to appear"
	if expectGone {
		waitingFor = "for preapproval to disappear"
	} else if oldCid != "" {
		waitingFor = fmt.Sprintf("for preapproval CID to change from %s", oldCid)
	}

	return nil, fmt.Errorf("timed out after %dms waiting %s", timeoutMs, waitingFor)
}

func (t *TokenStandardController) BuyMemberTraffic(
	ctx context.Context,
	buyer model.PartyID,
	ccAmount decimal.Decimal,
	memberId string,
	inputUtxos []string,
	migrationId int,
) (*CreateTransferResult, error) {
	syncID, err := t.GetSynchronizerID()
	if err != nil {
		return nil, err
	}

	cmd := &damlModel.Command{
		Command: &damlModel.ExerciseCommand{
			TemplateID: "Splice.AmuletRules:AmuletRules",
			Choice:     "AmuletRules_BuyMemberTraffic",
			Arguments: map[string]interface{}{
				"buyer":          string(buyer),
				"ccAmount":       ccAmount.String(),
				"memberId":       memberId,
				"inputs":         inputUtxos,
				"migrationId":    migrationId,
				"synchronizerId": string(syncID),
			},
		},
	}

	return &CreateTransferResult{
		Command:            cmd,
		DisclosedContracts: []*damlModel.DisclosedContract{},
	}, nil
}

func (t *TokenStandardController) GetMemberTrafficStatus(ctx context.Context, memberId string) (map[string]interface{}, error) {
	syncID, err := t.GetSynchronizerID()
	if err != nil {
		return nil, err
	}

	t.logger.Info().Str("synchronizerId", string(syncID)).Str("memberId", memberId).Msg("Getting member traffic status")
	return nil, fmt.Errorf("scan proxy API call not implemented - requires HTTP client")
}

type FeaturedAppRight struct {
	TemplateID       string
	ContractID       string
	Payload          map[string]interface{}
	CreatedEventBlob []byte
	CreatedAt        string
}

func (t *TokenStandardController) SelfGrantFeatureAppRights(ctx context.Context) (*CreateTransferResult, error) {
	partyID, err := t.GetPartyID()
	if err != nil {
		return nil, err
	}

	syncID, err := t.GetSynchronizerID()
	if err != nil {
		return nil, err
	}

	cmd := &damlModel.Command{
		Command: &damlModel.ExerciseCommand{
			TemplateID: "Splice.AmuletRules:AmuletRules",
			Choice:     "AmuletRules_DevNet_FeatureApp",
			Arguments: map[string]interface{}{
				"provider":       string(partyID),
				"synchronizerId": string(syncID),
			},
		},
	}

	return &CreateTransferResult{
		Command:            cmd,
		DisclosedContracts: []*damlModel.DisclosedContract{},
	}, nil
}

func (t *TokenStandardController) LookupFeaturedApps(ctx context.Context, maxRetries int, delayMs int) (*FeaturedAppRight, error) {
	if maxRetries == 0 {
		maxRetries = 10
	}
	if delayMs == 0 {
		delayMs = 5000
	}

	partyID, err := t.GetPartyID()
	if err != nil {
		return nil, err
	}

	for attempt := 1; attempt <= maxRetries; attempt++ {
		contracts, err := t.ListContractsByInterface(ctx, "Splice.Amulet:FeaturedAppRight")
		if err == nil && len(contracts) > 0 {
			for _, contract := range contracts {
				args, ok := contract.CreateArguments.(map[string]interface{})
				if !ok {
					continue
				}

				if provider, ok := args["provider"].(string); ok && provider == string(partyID) {
					return &FeaturedAppRight{
						TemplateID:       contract.TemplateID,
						ContractID:       contract.ContractID,
						Payload:          args,
						CreatedEventBlob: contract.CreatedEventBlob,
					}, nil
				}
			}
		}

		t.logger.Info().Int("attempt", attempt).Msg("Lookup featured apps returned undefined, retrying again...")

		if attempt < maxRetries {
			time.Sleep(time.Duration(delayMs) * time.Millisecond)
		}
	}

	return nil, nil
}

func (t *TokenStandardController) GrantFeatureAppRightsForInternalParty(ctx context.Context) (*FeaturedAppRight, error) {
	featuredAppRights, err := t.LookupFeaturedApps(ctx, 1, 1000)
	if err != nil {
		return nil, err
	}

	if featuredAppRights != nil {
		return featuredAppRights, nil
	}

	result, err := t.SelfGrantFeatureAppRights(ctx)
	if err != nil {
		return nil, err
	}

	partyID, err := t.GetPartyID()
	if err != nil {
		return nil, err
	}

	submitReq := &damlModel.SubmitRequest{
		Commands: &damlModel.Commands{
			UserID:    t.userID,
			CommandID: fmt.Sprintf("feature-app-%d", time.Now().UnixNano()),
			Commands:  []*damlModel.Command{result.Command},
			ActAs:     []string{string(partyID)},
			ReadAs:    []string{},
		},
	}

	_, err = t.damlClient.CommandSubmission.Submit(ctx, submitReq)
	if err != nil {
		return nil, fmt.Errorf("failed to submit feature app grant: %w", err)
	}

	return t.LookupFeaturedApps(ctx, 5, 1000)
}

func (t *TokenStandardController) CreateAndSubmitTapInternal(
	ctx context.Context,
	receiver model.PartyID,
	amount decimal.Decimal,
	instrumentID string,
	instrumentAdmin string,
) (map[string]interface{}, error) {
	result, err := t.CreateTap(ctx, receiver, amount, instrumentAdmin, instrumentID)
	if err != nil {
		return nil, err
	}

	partyID, err := t.GetPartyID()
	if err != nil {
		return nil, err
	}

	cmdID := fmt.Sprintf("tap-%d", time.Now().UnixNano())
	submitReq := &damlModel.SubmitAndWaitRequest{
		Commands: &damlModel.Commands{
			UserID:    t.userID,
			CommandID: cmdID,
			Commands:  []*damlModel.Command{result.Command},
			ActAs:     []string{string(partyID)},
			ReadAs:    []string{},
		},
	}

	resp, err := t.damlClient.CommandService.SubmitAndWait(ctx, submitReq)
	if err != nil {
		return nil, fmt.Errorf("failed to submit tap: %w", err)
	}

	return map[string]interface{}{
		"commandId":        cmdID,
		"updateId":         resp.UpdateID,
		"completionOffset": resp.CompletionOffset,
	}, nil
}

func (t *TokenStandardController) UseMergeDelegations(ctx context.Context, walletParty model.PartyID, nodeLimit int) (*CreateTransferResult, error) {
	if nodeLimit == 0 {
		nodeLimit = 200
	}

	utxos, err := t.ListHoldingUtxos(ctx, true, 100)
	if err != nil {
		return nil, err
	}

	if len(utxos) < 10 {
		return nil, fmt.Errorf("utxos are less than 10, found %d", len(utxos))
	}

	req := &damlModel.GetActiveContractsRequest{
		EventFormat: &damlModel.EventFormat{
			Verbose: true,
			FiltersByParty: map[string]*damlModel.Filters{
				string(walletParty): {
					Inclusive: &damlModel.InclusiveFilters{
						TemplateFilters: []*damlModel.TemplateFilter{
							{TemplateID: "Splice.MergeDelegation:MergeDelegation"},
						},
					},
				},
			},
		},
	}

	stream, errChan := t.damlClient.StateService.GetActiveContracts(ctx, req)

	var mergeDelegationCid string
	select {
	case resp := <-stream:
		if entry, ok := resp.ContractEntry.(*damlModel.ActiveContractEntry); ok {
			if entry.ActiveContract != nil && entry.ActiveContract.CreatedEvent != nil {
				mergeDelegationCid = entry.ActiveContract.CreatedEvent.ContractID
			}
		}
	case err := <-errChan:
		return nil, err
	}

	if mergeDelegationCid == "" {
		return nil, fmt.Errorf("merge delegation contract not found")
	}

	mergeResult, err := t.MergeHoldingUtxos(ctx, nodeLimit, walletParty, utxos)
	if err != nil {
		return nil, err
	}

	var mergeCalls []map[string]interface{}
	for _, cmd := range mergeResult.Commands {
		mergeCalls = append(mergeCalls, map[string]interface{}{
			"delegationCid": mergeDelegationCid,
			"choiceArg":     cmd,
		})
	}

	batchCmd := &damlModel.Command{
		Command: &damlModel.ExerciseCommand{
			TemplateID: "Splice.BatchMergeUtility:BatchMergeUtility",
			Choice:     "BatchMergeUtility_BatchMerge",
			Arguments: map[string]interface{}{
				"mergeCalls": mergeCalls,
			},
		},
	}

	return &CreateTransferResult{
		Command:            batchCmd,
		DisclosedContracts: mergeResult.DisclosedContracts,
	}, nil
}

func (t *TokenStandardController) CreateBatchMergeUtility(ctx context.Context) (*damlModel.Command, error) {
	partyID, err := t.GetPartyID()
	if err != nil {
		return nil, err
	}

	return &damlModel.Command{
		Command: &damlModel.CreateCommand{
			TemplateID: "Splice.BatchMergeUtility:BatchMergeUtility",
			Arguments: map[string]interface{}{
				"operator": string(partyID),
			},
		},
	}, nil
}

func (t *TokenStandardController) findAmuletRulesContract(ctx context.Context) (string, string, error) {
	templateID := t.GetAmuletRulesTemplateID()
	contractID := t.GetAmuletRulesContractID()

	if templateID == "" {
		templateID = os.Getenv("AMULET_RULES_TEMPLATE_ID")
	}
	if contractID == "" {
		contractID = os.Getenv("AMULET_RULES_CONTRACT_ID")
	}

	if templateID != "" && contractID != "" {
		t.logger.Info().
			Str("templateID", templateID).
			Str("contractID", contractID).
			Msg("Using AmuletRules contract from configured values")
		return templateID, contractID, nil
	}

	packages, err := t.damlClient.PackageMng.ListKnownPackages(ctx)
	if err != nil {
		return "", "", fmt.Errorf("failed to list packages: %w", err)
	}

	var spliceAmuletPkgID string
	for _, pkg := range packages {
		if pkg.Name == "splice-amulet" {
			spliceAmuletPkgID = pkg.PackageID
			break
		}
	}

	if spliceAmuletPkgID == "" {
		return "", "", fmt.Errorf("splice-amulet package not found")
	}

	possibleTemplateIDs := []string{
		fmt.Sprintf("%s:Splice.AmuletRules:AmuletRules", spliceAmuletPkgID),
		fmt.Sprintf("%s:Splice.Amulet:AmuletRules", spliceAmuletPkgID),
		fmt.Sprintf("%s:Splice.Amulet.AmuletRules:AmuletRules", spliceAmuletPkgID),
	}

	partyID, err := t.GetPartyID()
	if err != nil {
		return "", "", err
	}

	for _, templateID := range possibleTemplateIDs {
		t.logger.Info().
			Str("templateID", templateID).
			Str("partyID", string(partyID)).
			Msg("Trying to find AmuletRules with template ID")

		req := &damlModel.GetActiveContractsRequest{
			EventFormat: &damlModel.EventFormat{
				Verbose: true,
				FiltersByParty: map[string]*damlModel.Filters{
					string(partyID): {
						Inclusive: &damlModel.InclusiveFilters{
							TemplateFilters: []*damlModel.TemplateFilter{
								{
									TemplateID:              templateID,
									IncludeCreatedEventBlob: false,
								},
							},
						},
					},
				},
			},
		}

		stream, errChan := t.damlClient.StateService.GetActiveContracts(ctx, req)

		var foundContract *damlModel.CreatedEvent
	streamLoop:
		for {
			select {
			case resp, ok := <-stream:
				if !ok {
					if foundContract != nil {
						t.logger.Info().
							Str("templateID", templateID).
							Str("contractID", foundContract.ContractID).
							Msg("Found AmuletRules contract")
						return templateID, foundContract.ContractID, nil
					}
					t.logger.Debug().
						Str("templateID", templateID).
						Str("partyID", string(partyID)).
						Msg("Stream closed, no contract found with this template ID")
					break streamLoop
				}
				if entry, ok := resp.ContractEntry.(*damlModel.ActiveContractEntry); ok {
					if entry.ActiveContract != nil && entry.ActiveContract.CreatedEvent != nil {
						contract := entry.ActiveContract.CreatedEvent
						t.logger.Info().
							Str("templateID", templateID).
							Str("contractID", contract.ContractID).
							Str("partyID", string(partyID)).
							Msg("Received contract from stream")
						foundContract = contract
					}
				}
			case err := <-errChan:
				if err != nil {
					t.logger.Warn().
						Err(err).
						Str("templateID", templateID).
						Str("partyID", string(partyID)).
						Msg("Error querying for template, trying next")
					break streamLoop
				}
			case <-ctx.Done():
				return "", "", ctx.Err()
			}
		}
	}

	return "", "", fmt.Errorf("AmuletRules contract not found - it may need to be initialized first. Attempted template IDs: %v", possibleTemplateIDs)
}

func (t *TokenStandardController) CreateMergeDelegationProposal(ctx context.Context, delegate model.PartyID, metadata map[string]interface{}) (*damlModel.Command, error) {
	partyID, err := t.GetPartyID()
	if err != nil {
		return nil, err
	}

	if metadata == nil {
		metadata = map[string]interface{}{"values": map[string]interface{}{}}
	}

	return &damlModel.Command{
		Command: &damlModel.CreateCommand{
			TemplateID: "Splice.MergeDelegationProposal:MergeDelegationProposal",
			Arguments: map[string]interface{}{
				"delegation": map[string]interface{}{
					"operator": string(delegate),
					"owner":    string(partyID),
					"meta":     metadata,
				},
			},
		},
	}, nil
}

func (t *TokenStandardController) LookupMergeDelegationProposal(ctx context.Context, ownerParty model.PartyID) ([]*damlModel.CreatedEvent, error) {
	if ownerParty == "" {
		var err error
		ownerParty, err = t.GetPartyID()
		if err != nil {
			return nil, err
		}
	}

	req := &damlModel.GetActiveContractsRequest{
		EventFormat: &damlModel.EventFormat{
			Verbose: true,
			FiltersByParty: map[string]*damlModel.Filters{
				string(ownerParty): {
					Inclusive: &damlModel.InclusiveFilters{
						TemplateFilters: []*damlModel.TemplateFilter{
							{TemplateID: "Splice.MergeDelegationProposal:MergeDelegationProposal"},
						},
					},
				},
			},
		},
	}

	stream, errChan := t.damlClient.StateService.GetActiveContracts(ctx, req)

	var contracts []*damlModel.CreatedEvent
	for {
		select {
		case resp, ok := <-stream:
			if !ok {
				return contracts, nil
			}
			if entry, ok := resp.ContractEntry.(*damlModel.ActiveContractEntry); ok {
				if entry.ActiveContract != nil && entry.ActiveContract.CreatedEvent != nil {
					contracts = append(contracts, entry.ActiveContract.CreatedEvent)
				}
			}
		case err := <-errChan:
			if err != nil {
				return nil, err
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (t *TokenStandardController) ApproveMergeDelegationProposal(ctx context.Context, ownerParty model.PartyID) (*CreateTransferResult, error) {
	proposals, err := t.LookupMergeDelegationProposal(ctx, ownerParty)
	if err != nil {
		return nil, err
	}

	if len(proposals) == 0 {
		return nil, fmt.Errorf("no merge delegation proposal found")
	}

	proposal := proposals[0]

	cmd := &damlModel.Command{
		Command: &damlModel.ExerciseCommand{
			ContractID: proposal.ContractID,
			TemplateID: "Splice.MergeDelegationProposal:MergeDelegationProposal",
			Choice:     "MergeDelegationProposal_Accept",
			Arguments:  map[string]interface{}{},
		},
	}

	disclosed := &damlModel.DisclosedContract{
		TemplateID:       proposal.TemplateID,
		ContractID:       proposal.ContractID,
		CreatedEventBlob: proposal.CreatedEventBlob,
	}

	return &CreateTransferResult{
		Command:            cmd,
		DisclosedContracts: []*damlModel.DisclosedContract{disclosed},
	}, nil
}
