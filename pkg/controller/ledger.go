package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/noders-team/go-daml/pkg/client"
	damlModel "github.com/noders-team/go-daml/pkg/model"
	"github.com/noders-team/go-wallet-daml/pkg/auth"
	"github.com/noders-team/go-wallet-daml/pkg/crypto"
	"github.com/noders-team/go-wallet-daml/pkg/model"
	"github.com/noders-team/go-wallet-daml/pkg/wrapper"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type LedgerController struct {
	damlClient     *client.DamlBindingClient
	ledgerWrapper  *wrapper.LedgerWrapper
	userID         string
	isAdmin        bool
	partyID        atomic.Value
	synchronizerID atomic.Value
	logger         zerolog.Logger
	initOnce       sync.Once
	initErr        error
}

func NewLedgerController(userID string, grpcAddress string, httpBaseURL string, provider *auth.AuthTokenProvider, isAdmin bool) (*LedgerController, error) {
	logger := log.Logger.With().
		Str("component", "ledger-controller").
		Str("userID", userID).
		Bool("isAdmin", isAdmin).
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

	ledgerWrapper := wrapper.NewLedgerWrapper(httpBaseURL, provider)

	lc := &LedgerController{
		userID:        userID,
		isAdmin:       isAdmin,
		ledgerWrapper: ledgerWrapper,
		logger:        logger,
	}

	ctx := context.Background()
	conn, err := damlCl.Connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to DAML ledger: %w", err)
	}

	lc.damlClient = client.NewDamlBindingClient(&client.DamlClient{}, conn.GRPCConn())

	return lc, nil
}

func (l *LedgerController) AwaitInit(ctx context.Context) error {
	l.initOnce.Do(func() {
		l.initErr = l.damlClient.Ping(ctx)
	})
	return l.initErr
}

func (l *LedgerController) SetPartyID(partyID model.PartyID) {
	l.partyID.Store(partyID)
}

func (l *LedgerController) SetSynchronizerID(synchronizerID model.PartyID) {
	l.synchronizerID.Store(synchronizerID)
}

func (l *LedgerController) GetPartyID() (model.PartyID, error) {
	v := l.partyID.Load()
	if v == nil {
		return "", fmt.Errorf("partyID not set")
	}
	return v.(model.PartyID), nil
}

func (l *LedgerController) GetSynchronizerID() (model.PartyID, error) {
	v := l.synchronizerID.Load()
	if v == nil {
		return "", fmt.Errorf("synchronizerID not set")
	}
	return v.(model.PartyID), nil
}

func (l *LedgerController) PrepareSubmission(ctx context.Context, commands interface{}, commandID string, disclosedContracts []*model.DisclosedContract) (*model.PrepareSubmissionResponse, error) {
	partyID, err := l.GetPartyID()
	if err != nil {
		return nil, err
	}

	syncID, err := l.GetSynchronizerID()
	if err != nil {
		return nil, err
	}

	if commandID == "" {
		commandID = uuid.New().String()
	}

	var damlCommands []*damlModel.Command
	switch cmds := commands.(type) {
	case []*model.WrappedCommand:
		for _, cmd := range cmds {
			damlCommands = append(damlCommands, convertWrappedCommand(cmd))
		}
	case *model.WrappedCommand:
		damlCommands = append(damlCommands, convertWrappedCommand(cmds))
	default:
		return nil, fmt.Errorf("unsupported command type")
	}

	var damlDisclosed []*damlModel.DisclosedContract
	for _, dc := range disclosedContracts {
		damlDisclosed = append(damlDisclosed, &damlModel.DisclosedContract{
			TemplateID:       dc.TemplateID,
			ContractID:       dc.ContractID,
			CreatedEventBlob: dc.CreatedEventBlob,
			SynchronizerID:   dc.SynchronizerID,
		})
	}

	req := &damlModel.PrepareSubmissionRequest{
		UserID:           l.userID,
		CommandID:        commandID,
		Commands:         damlCommands,
		ActAs:            []string{string(partyID)},
		ReadAs:           []string{},
		DisclosedContracts: damlDisclosed,
		SynchronizerID:   string(syncID),
	}

	resp, err := l.damlClient.InteractiveSubmissionService.PrepareSubmission(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare submission: %w", err)
	}

	return &model.PrepareSubmissionResponse{
		PreparedTransaction:     resp.PreparedTransaction,
		PreparedTransactionHash: base64.StdEncoding.EncodeToString(resp.PreparedTransactionHash),
	}, nil
}

func (l *LedgerController) ExecuteSubmission(ctx context.Context, prepared *model.PrepareSubmissionResponse, signature string, publicKey string, commandID string) (string, error) {
	partyID, err := l.GetPartyID()
	if err != nil {
		return "", err
	}

	fingerprint, err := crypto.CreateFingerprintFromKey(publicKey)
	if err != nil {
		return "", fmt.Errorf("failed to create fingerprint: %w", err)
	}

	signatureBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return "", fmt.Errorf("failed to decode signature: %w", err)
	}

	req := &damlModel.ExecuteSubmissionRequest{
		UserID:              l.userID,
		PreparedTransaction: prepared.PreparedTransaction,
		PartySignatures: []*damlModel.SinglePartySignatures{
			{
				Party: string(partyID),
				Signatures: []*damlModel.Signature{
					{
						Format:               damlModel.SignatureFormatConcat,
						Signature:            signatureBytes,
						SignedBy:             fingerprint,
						SigningAlgorithmSpec: damlModel.SigningAlgorithmSpecED25519,
					},
				},
			},
		},
		SubmissionID: commandID,
	}

	_, err = l.damlClient.InteractiveSubmissionService.ExecuteSubmission(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to execute submission: %w", err)
	}

	return commandID, nil
}

func (l *LedgerController) PrepareSignAndExecuteTransaction(ctx context.Context, commands interface{}, privateKey string, commandID string, disclosedContracts []*model.DisclosedContract) (string, error) {
	prepared, err := l.PrepareSubmission(ctx, commands, commandID, disclosedContracts)
	if err != nil {
		return "", err
	}

	signature, err := crypto.SignTransactionHash(prepared.PreparedTransactionHash, privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign transaction: %w", err)
	}

	publicKey, err := crypto.GetPublicKeyFromPrivate(privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to get public key: %w", err)
	}

	return l.ExecuteSubmission(ctx, prepared, signature, publicKey, commandID)
}

func (l *LedgerController) PrepareSignExecuteAndWaitFor(ctx context.Context, commands interface{}, privateKey string, commandID string, disclosedContracts []*model.DisclosedContract, timeoutMs int) (*model.CompletionValue, error) {
	ledgerEnd, err := l.LedgerEnd(ctx)
	if err != nil {
		return nil, err
	}

	submissionID, err := l.PrepareSignAndExecuteTransaction(ctx, commands, privateKey, commandID, disclosedContracts)
	if err != nil {
		return nil, err
	}

	return l.WaitForCompletion(ctx, ledgerEnd, timeoutMs, submissionID)
}

func (l *LedgerController) WaitForCompletion(ctx context.Context, ledgerEnd int64, timeoutMs int, commandID string) (*model.CompletionValue, error) {
	partyID, err := l.GetPartyID()
	if err != nil {
		return nil, err
	}

	req := &damlModel.CompletionStreamRequest{
		UserID:         l.userID,
		Parties:        []string{string(partyID)},
		BeginExclusive: ledgerEnd,
	}

	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)

	responseChan, errorChan := l.damlClient.CommandCompletion.CompletionStream(ctx, req)

	for time.Now().Before(deadline) {
		select {
		case resp, ok := <-responseChan:
			if !ok {
				return nil, fmt.Errorf("completion stream closed")
			}

			if completion, ok := resp.Response.(*damlModel.Completion); ok {
				if completion.CommandID == commandID {
					return &model.CompletionValue{
						CommandID:     completion.CommandID,
						UpdateID:      completion.UpdateID,
						TransactionID: completion.TransactionID,
						SubmissionID:  completion.SubmissionID,
						CompletedAt:   completion.CompletedAt,
						Offset:        completion.Offset,
					}, nil
				}
			}
		case err := <-errorChan:
			if err != nil {
				return nil, fmt.Errorf("completion stream error: %w", err)
			}
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, fmt.Errorf("timeout waiting for completion")
}

func (l *LedgerController) AllocateInternalParty(ctx context.Context, partyHint string) (model.PartyID, error) {
	if partyHint == "" {
		partyHint = uuid.New().String()
	}

	resp, err := l.damlClient.PartyMng.AllocateParty(ctx, partyHint, nil, "")
	if err != nil {
		return "", fmt.Errorf("failed to allocate party: %w", err)
	}

	return model.PartyID(resp.Party), nil
}

func (l *LedgerController) GenerateExternalParty(ctx context.Context, publicKey string, partyHint string, confirmingThreshold int, confirmingParticipantUIDs []string, observingParticipantUIDs []string) (*model.GenerateTransactionResponse, error) {
	syncID, err := l.GetSynchronizerID()
	if err != nil {
		return nil, err
	}

	if partyHint == "" {
		partyHint = uuid.New().String()
	}

	return l.ledgerWrapper.GenerateExternalPartyTopology(
		ctx,
		string(syncID),
		publicKey,
		partyHint,
		false,
		int32(confirmingThreshold),
		confirmingParticipantUIDs,
		observingParticipantUIDs,
	)
}

func (l *LedgerController) AllocateExternalParty(ctx context.Context, signedHash string, preparedParty *model.GenerateTransactionResponse, grantUserRights bool, confirmingEndpoints []*model.ParticipantEndpointConfig, observingEndpoints []*model.ParticipantEndpointConfig, expectHeavyLoad bool) (*model.AllocateExternalPartyResponse, error) {
	syncID, err := l.GetSynchronizerID()
	if err != nil {
		return nil, err
	}

	transactions := make([]*model.TopologyTransaction, len(preparedParty.TopologyTransactions))
	for i, tx := range preparedParty.TopologyTransactions {
		transactions[i] = &model.TopologyTransaction{Transaction: tx}
	}

	signatures := []*model.Signature{
		{
			Format:               "SIGNATURE_FORMAT_CONCAT",
			Signature:            signedHash,
			SignedBy:             preparedParty.PublicKeyFingerprint,
			SigningAlgorithmSpec: "SIGNING_ALGORITHM_SPEC_ED25519",
		},
	}

	partyID, err := l.ledgerWrapper.AllocateExternalParty(ctx, string(syncID), transactions, signatures)
	if err != nil {
		return nil, err
	}

	if grantUserRights {
		rights := []*damlModel.Right{
			{Type: damlModel.CanActAs{Party: partyID}},
			{Type: damlModel.CanReadAs{Party: partyID}},
		}
		_, err = l.damlClient.UserMng.GrantUserRights(ctx, l.userID, "", rights)
		if err != nil {
			l.logger.Warn().Err(err).Msg("Failed to grant user rights")
		}
	}

	return &model.AllocateExternalPartyResponse{PartyID: partyID}, nil
}

func (l *LedgerController) SignAndAllocateExternalParty(ctx context.Context, privateKey string, partyHint string, confirmingThreshold int, confirmingEndpoints []*model.ParticipantEndpointConfig, observingEndpoints []*model.ParticipantEndpointConfig, grantUserRights bool) (*model.GenerateTransactionResponse, error) {
	publicKey, err := crypto.GetPublicKeyFromPrivate(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get public key: %w", err)
	}

	var confirmingUIDs []string
	var observingUIDs []string

	preparedParty, err := l.GenerateExternalParty(ctx, publicKey, partyHint, confirmingThreshold, confirmingUIDs, observingUIDs)
	if err != nil {
		return nil, err
	}

	signedHash, err := crypto.SignTransactionHash(preparedParty.MultiHash, privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign multi-hash: %w", err)
	}

	_, err = l.AllocateExternalParty(ctx, signedHash, preparedParty, grantUserRights, confirmingEndpoints, observingEndpoints, true)
	if err != nil {
		return nil, err
	}

	return preparedParty, nil
}

func (l *LedgerController) SignAndAllocateExternalPartyWithPreapproval(ctx context.Context, privateKey string, providerParty model.PartyID, dsoParty model.PartyID, partyHint string, confirmingThreshold int, confirmingEndpoints []*model.ParticipantEndpointConfig, observingEndpoints []*model.ParticipantEndpointConfig, grantUserRights bool) (*model.GenerateTransactionResponse, error) {
	allocatedParty, err := l.SignAndAllocateExternalParty(ctx, privateKey, partyHint, confirmingThreshold, confirmingEndpoints, observingEndpoints, grantUserRights)
	if err != nil {
		return nil, err
	}

	oldPartyID, _ := l.GetPartyID()
	l.SetPartyID(model.PartyID(allocatedParty.PartyID))

	transferPreapprovalCmd, err := l.CreateTransferPreapprovalCommand(ctx, providerParty, model.PartyID(allocatedParty.PartyID), dsoParty)
	if err != nil {
		l.SetPartyID(oldPartyID)
		return nil, err
	}

	_, err = l.PrepareSignExecuteAndWaitFor(ctx, []*model.WrappedCommand{transferPreapprovalCmd}, privateKey, uuid.New().String(), nil, 15000)
	if err != nil {
		l.SetPartyID(oldPartyID)
		return nil, fmt.Errorf("failed to create transfer preapproval: %w", err)
	}

	l.SetPartyID(oldPartyID)
	return allocatedParty, nil
}

func (l *LedgerController) CreateTransferPreapprovalCommand(ctx context.Context, providerParty model.PartyID, receiverParty model.PartyID, dsoParty model.PartyID) (*model.WrappedCommand, error) {
	return &model.WrappedCommand{
		CreateCommand: &model.CreateCommand{
			TemplateID: "#splice-wallet:Splice.Wallet.TransferPreapproval:TransferPreapprovalProposal",
			CreateArguments: map[string]interface{}{
				"provider":    providerParty,
				"receiver":    receiverParty,
				"expectedDso": dsoParty,
			},
		},
	}, nil
}

func (l *LedgerController) LedgerEnd(ctx context.Context) (int64, error) {
	resp, err := l.damlClient.StateService.GetLedgerEnd(ctx, &damlModel.GetLedgerEndRequest{})
	if err != nil {
		return 0, err
	}
	return resp.Offset, nil
}

func (l *LedgerController) ListSynchronizers(ctx context.Context, partyID model.PartyID) (*model.ListSynchronizersResponse, error) {
	resp, err := l.damlClient.StateService.GetConnectedSynchronizers(ctx, &damlModel.GetConnectedSynchronizersRequest{})
	if err != nil {
		return nil, err
	}

	result := &model.ListSynchronizersResponse{
		ConnectedSynchronizers: make([]*model.SynchronizerInfo, len(resp.ConnectedSynchronizers)),
	}

	for i, sync := range resp.ConnectedSynchronizers {
		result.ConnectedSynchronizers[i] = &model.SynchronizerInfo{
			SynchronizerID: sync.SynchronizerID,
			Permission:     model.ParticipantPermission(sync.ParticipantPermission),
		}
	}

	return result, nil
}

func (l *LedgerController) VerifyTxHash(txHash string, publicKey string, signature string) bool {
	return crypto.VerifySignedTxHash(txHash, publicKey, signature)
}

func (l *LedgerController) GetActiveContracts(ctx context.Context, filter *model.TransactionFilter) ([]*model.ActiveContract, error) {
	var damlFilter *damlModel.TransactionFilter
	if filter != nil {
		damlFilter = &damlModel.TransactionFilter{
			FiltersByParty: make(map[string]*damlModel.Filters),
		}
		for party, filters := range filter.FiltersByParty {
			if filters != nil && filters.Inclusive != nil {
				templateFilters := make([]*damlModel.TemplateFilter, len(filters.Inclusive.TemplateFilters))
				for i, tf := range filters.Inclusive.TemplateFilters {
					templateFilters[i] = &damlModel.TemplateFilter{
						TemplateID:              tf.TemplateID,
						IncludeCreatedEventBlob: tf.IncludeCreatedEventBlob,
					}
				}
				damlFilter.FiltersByParty[party] = &damlModel.Filters{
					Inclusive: &damlModel.InclusiveFilters{
						TemplateFilters: templateFilters,
					},
				}
			}
		}
	}

	req := &damlModel.GetActiveContractsRequest{
		Filter: damlFilter,
	}

	var activeContracts []*model.ActiveContract

	stream, errChan := l.damlClient.StateService.GetActiveContracts(ctx, req)

	for {
		select {
		case resp, ok := <-stream:
			if !ok {
				return activeContracts, nil
			}
			for _, ac := range resp.ActiveContracts {
				createArgs, _ := ac.CreateArguments.(map[string]interface{})
				activeContracts = append(activeContracts, &model.ActiveContract{
					ContractID:       ac.ContractID,
					TemplateID:       ac.TemplateID,
					CreateArguments:  createArgs,
					Signatories:      ac.Signatories,
					Observers:        ac.Observers,
					CreatedAt:        ac.CreatedAt,
					ContractKey:      ac.ContractKey,
					WitnessParties:   ac.WitnessParties,
					CreatedEventBlob: ac.CreatedEventBlob,
				})
			}
		case err := <-errChan:
			if err != nil {
				return nil, fmt.Errorf("failed to get active contracts: %w", err)
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (l *LedgerController) GetTransactionTrees(ctx context.Context, filter *model.TransactionFilter, beginOffset string, endOffset *string) ([]*model.TransactionTree, error) {
	var damlFilter *damlModel.TransactionFilter
	if filter != nil {
		damlFilter = &damlModel.TransactionFilter{
			FiltersByParty: make(map[string]*damlModel.Filters),
		}
		for party, filters := range filter.FiltersByParty {
			if filters != nil && filters.Inclusive != nil {
				templateFilters := make([]*damlModel.TemplateFilter, len(filters.Inclusive.TemplateFilters))
				for i, tf := range filters.Inclusive.TemplateFilters {
					templateFilters[i] = &damlModel.TemplateFilter{
						TemplateID:              tf.TemplateID,
						IncludeCreatedEventBlob: tf.IncludeCreatedEventBlob,
					}
				}
				damlFilter.FiltersByParty[party] = &damlModel.Filters{
					Inclusive: &damlModel.InclusiveFilters{
						TemplateFilters: templateFilters,
					},
				}
			}
		}
	}

	beginOffsetInt := int64(0)
	if beginOffset != "" {
		fmt.Sscanf(beginOffset, "%d", &beginOffsetInt)
	}

	var endOffsetPtr *int64
	if endOffset != nil {
		endOffsetInt := int64(0)
		fmt.Sscanf(*endOffset, "%d", &endOffsetInt)
		endOffsetPtr = &endOffsetInt
	}

	req := &damlModel.GetUpdatesRequest{
		BeginExclusive: beginOffsetInt,
		EndInclusive:   endOffsetPtr,
		Filter:         damlFilter,
	}

	var transactions []*model.TransactionTree

	stream, errChan := l.damlClient.UpdateService.GetUpdates(ctx, req)

	for {
		select {
		case resp, ok := <-stream:
			if !ok {
				return transactions, nil
			}
			if resp.Update != nil && resp.Update.Transaction != nil {
				tx := resp.Update.Transaction
				events := make([]*model.Event, len(tx.Events))
				for i, evt := range tx.Events {
					events[i] = &model.Event{
						Created:   convertCreatedEvent(evt.Created),
						Archived:  convertArchivedEvent(evt.Archived),
						Exercised: convertExercisedEvent(evt.Exercised),
					}
				}
				transactions = append(transactions, &model.TransactionTree{
					UpdateID:    tx.UpdateID,
					CommandID:   tx.CommandID,
					WorkflowID:  tx.WorkflowID,
					EffectiveAt: tx.EffectiveAt,
					Events:      events,
					Offset:      tx.Offset,
				})
			}
		case err := <-errChan:
			if err != nil {
				return nil, fmt.Errorf("failed to get transaction trees: %w", err)
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func convertCreatedEvent(evt *damlModel.CreatedEvent) *model.CreatedEvent {
	if evt == nil {
		return nil
	}
	return &model.CreatedEvent{
		Offset:           evt.Offset,
		NodeID:           evt.NodeID,
		ContractID:       evt.ContractID,
		TemplateID:       evt.TemplateID,
		ContractKey:      evt.ContractKey,
		CreateArguments:  evt.CreateArguments,
		CreatedEventBlob: evt.CreatedEventBlob,
		WitnessParties:   evt.WitnessParties,
		Signatories:      evt.Signatories,
		Observers:        evt.Observers,
		CreatedAt:        evt.CreatedAt,
		PackageName:      evt.PackageName,
	}
}

func convertArchivedEvent(evt *damlModel.ArchivedEvent) *model.ArchivedEvent {
	if evt == nil {
		return nil
	}
	return &model.ArchivedEvent{
		Offset:         evt.Offset,
		NodeID:         evt.NodeID,
		ContractID:     evt.ContractID,
		TemplateID:     evt.TemplateID,
		WitnessParties: evt.WitnessParties,
		PackageName:    evt.PackageName,
	}
}

func convertExercisedEvent(evt *damlModel.ExercisedEvent) *model.ExercisedEvent {
	if evt == nil {
		return nil
	}
	return &model.ExercisedEvent{
		Offset:         evt.Offset,
		NodeID:         evt.NodeID,
		ContractID:     evt.ContractID,
		TemplateID:     evt.TemplateID,
		InterfaceID:    evt.InterfaceID,
		Choice:         evt.Choice,
		ChoiceArgument: evt.ChoiceArgument,
		ActingParties:  evt.ActingParties,
		Consuming:      evt.Consuming,
		WitnessParties: evt.WitnessParties,
		ExerciseResult: evt.ExerciseResult,
		PackageName:    evt.PackageName,
	}
}

func convertWrappedCommand(cmd *model.WrappedCommand) *damlModel.Command {
	damlCmd := &damlModel.Command{}

	if cmd.CreateCommand != nil {
		damlCmd.Command = damlModel.CreateCommand{
			TemplateID: cmd.CreateCommand.TemplateID,
			Arguments:  cmd.CreateCommand.CreateArguments,
		}
	} else if cmd.ExerciseCommand != nil {
		damlCmd.Command = damlModel.ExerciseCommand{
			ContractID: cmd.ExerciseCommand.ContractID,
			TemplateID: cmd.ExerciseCommand.TemplateID,
			Choice:     cmd.ExerciseCommand.Choice,
			Arguments:  cmd.ExerciseCommand.ChoiceArguments,
		}
	}

	return damlCmd
}
