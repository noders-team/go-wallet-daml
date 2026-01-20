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
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type LedgerController struct {
	damlClient     *client.DamlBindingClient
	userID         string
	isAdmin        bool
	partyID        atomic.Value
	synchronizerID atomic.Value
	logger         zerolog.Logger
	initOnce       sync.Once
	initErr        error
}

type LedgerControllerBuilder struct {
	userID         string
	grpcAddress    string
	httpBaseURL    string
	provider       *auth.AuthTokenProvider
	token          *string
	isAdmin        bool
	logger         *zerolog.Logger
	tlsConfig      *client.TLSConfig
	connectOnBuild bool
}

func NewLedgerControllerBuilder() *LedgerControllerBuilder {
	return &LedgerControllerBuilder{
		connectOnBuild: true, // default to connecting on build
	}
}

func (b *LedgerControllerBuilder) WithUserID(userID string) *LedgerControllerBuilder {
	b.userID = userID
	return b
}

func (b *LedgerControllerBuilder) WithGRPCAddress(grpcAddress string) *LedgerControllerBuilder {
	b.grpcAddress = grpcAddress
	return b
}

func (b *LedgerControllerBuilder) WithHTTPBaseURL(httpBaseURL string) *LedgerControllerBuilder {
	b.httpBaseURL = httpBaseURL
	return b
}

func (b *LedgerControllerBuilder) WithAuthProvider(provider *auth.AuthTokenProvider) *LedgerControllerBuilder {
	b.provider = provider
	return b
}

func (b *LedgerControllerBuilder) WithToken(token string) *LedgerControllerBuilder {
	b.token = &token
	return b
}

func (b *LedgerControllerBuilder) WithAdminMode(isAdmin bool) *LedgerControllerBuilder {
	b.isAdmin = isAdmin
	return b
}

func (b *LedgerControllerBuilder) WithLogger(logger zerolog.Logger) *LedgerControllerBuilder {
	b.logger = &logger
	return b
}

func (b *LedgerControllerBuilder) WithTLSConfig(tlsConfig *client.TLSConfig) *LedgerControllerBuilder {
	b.tlsConfig = tlsConfig
	return b
}

func (b *LedgerControllerBuilder) WithConnectOnBuild(connect bool) *LedgerControllerBuilder {
	b.connectOnBuild = connect
	return b
}

func (b *LedgerControllerBuilder) Build(ctx context.Context) (*LedgerController, error) {
	var logger zerolog.Logger
	if b.logger != nil {
		logger = *b.logger
	} else {
		logger = log.Logger.With().
			Str("component", "ledger-controller").
			Str("userID", b.userID).
			Bool("isAdmin", b.isAdmin).
			Logger()
	}

	damlConfig := &client.Config{
		Address: b.grpcAddress,
		TLS:     b.tlsConfig,
	}
	if b.token != nil {
		damlConfig.Auth = &client.AuthConfig{Token: *b.token}
	} else {
		tokenProviderFunc := func() (string, error) {
			ctx := context.Background()
			return b.provider.GetUserAccessToken(ctx)
		}

		damlConfig.Auth = &client.AuthConfig{TokenProvider: tokenProviderFunc}
	}

	damlCl := client.NewClient(damlConfig)

	lc := &LedgerController{
		userID:         b.userID,
		isAdmin:        b.isAdmin,
		logger:         logger,
		synchronizerID: atomic.Value{},
		partyID:        atomic.Value{},
	}

	if b.connectOnBuild {
		conn, err := damlCl.Connect(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to DAML ledger: %w", err)
		}
		lc.damlClient = client.NewDamlBindingClient(&client.DamlClient{}, conn)
	}

	return lc, nil
}

// NewLedgerController creates a new LedgerController with default configuration
// Deprecated: Use NewLedgerControllerBuilder for more flexibility
func NewLedgerController(userID string,
	grpcAddress string,
	httpBaseURL string,
	provider *auth.AuthTokenProvider, isAdmin bool,
) (*LedgerController, error) {
	return NewLedgerControllerBuilder().
		WithUserID(userID).
		WithGRPCAddress(grpcAddress).
		WithHTTPBaseURL(httpBaseURL).
		WithAuthProvider(provider).
		WithAdminMode(isAdmin).
		Build(context.Background())
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

func (l *LedgerController) PrepareSubmission(ctx context.Context, commands interface{}, commandID string, disclosedContracts []*model.DisclosedContract, actAs []string, readAs []string) (*model.PrepareSubmissionResponse, error) {
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
	case []*damlModel.Command:
		damlCommands = cmds
	case *damlModel.Command:
		damlCommands = []*damlModel.Command{cmds}
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

	if len(actAs) == 0 {
		actAs = []string{string(partyID)}
	}
	if readAs == nil {
		readAs = []string{}
	}

	req := &damlModel.PrepareSubmissionRequest{
		UserID:             l.userID,
		CommandID:          commandID,
		Commands:           damlCommands,
		ActAs:              actAs,
		ReadAs:             readAs,
		DisclosedContracts: damlDisclosed,
		SynchronizerID:     string(syncID),
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

func (l *LedgerController) ExecuteSubmissionAndWaitFor(ctx context.Context, prepared *model.PrepareSubmissionResponse, signature string, publicKey string, submissionID string, timeoutMs int) (*model.CompletionValue, error) {
	ledgerEnd, err := l.LedgerEnd(ctx)
	if err != nil {
		return nil, err
	}

	commandID, err := l.ExecuteSubmission(ctx, prepared, signature, publicKey, submissionID)
	if err != nil {
		return nil, err
	}

	return l.WaitForCompletion(ctx, ledgerEnd, timeoutMs, commandID)
}

func (l *LedgerController) SubmitCommand(ctx context.Context, commands interface{}, commandID string, disclosedContracts []*model.DisclosedContract) (string, error) {
	partyID, err := l.GetPartyID()
	if err != nil {
		return "", err
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
	case []*damlModel.Command:
		damlCommands = cmds
	case *damlModel.Command:
		damlCommands = []*damlModel.Command{cmds}
	default:
		return "", fmt.Errorf("unsupported command type")
	}

	req := &damlModel.SubmitRequest{
		Commands: &damlModel.Commands{
			UserID:    l.userID,
			CommandID: commandID,
			Commands:  damlCommands,
			ActAs:     []string{string(partyID)},
			ReadAs:    []string{},
		},
	}

	_, err = l.damlClient.CommandSubmission.Submit(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to submit command: %w", err)
	}

	l.logger.Info().
		Str("commandID", commandID).
		Str("partyID", string(partyID)).
		Msg("Command submitted successfully")

	return commandID, nil
}

type SubmitResult struct {
	UpdateID         string
	CompletionOffset int64
}

func (l *LedgerController) SubmitCommandAndWait(ctx context.Context, commands interface{}, commandID string, disclosedContracts []*model.DisclosedContract, actAs []string, readAs []string) (*SubmitResult, error) {
	partyID, err := l.GetPartyID()
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
	case []*damlModel.Command:
		damlCommands = cmds
	case *damlModel.Command:
		damlCommands = []*damlModel.Command{cmds}
	default:
		return nil, fmt.Errorf("unsupported command type")
	}

	if len(actAs) == 0 {
		actAs = []string{string(partyID)}
	}
	if readAs == nil {
		readAs = []string{}
	}

	req := &damlModel.SubmitAndWaitRequest{
		Commands: &damlModel.Commands{
			UserID:    l.userID,
			CommandID: commandID,
			Commands:  damlCommands,
			ActAs:     actAs,
			ReadAs:    readAs,
		},
	}

	resp, err := l.damlClient.CommandService.SubmitAndWait(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to submit and wait: %w", err)
	}

	l.logger.Info().
		Str("commandID", commandID).
		Str("updateID", resp.UpdateID).
		Int64("completionOffset", resp.CompletionOffset).
		Msg("Command submitted and executed successfully")

	return &SubmitResult{
		UpdateID:         resp.UpdateID,
		CompletionOffset: resp.CompletionOffset,
	}, nil
}

func (l *LedgerController) PrepareSignAndExecuteTransaction(ctx context.Context, commands interface{}, privateKey string, commandID string, disclosedContracts []*model.DisclosedContract) (string, error) {
	prepared, err := l.PrepareSubmission(ctx, commands, commandID, disclosedContracts, nil, nil)
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

func (l *LedgerController) AllocateExternalParty(ctx context.Context, signedHash string, preparedParty *model.GenerateTransactionResponse, grantUserRights bool, confirmingEndpoints []*model.ParticipantEndpointConfig, observingEndpoints []*model.ParticipantEndpointConfig, expectHeavyLoad bool) (*model.AllocateExternalPartyResponse, error) {
	syncID, err := l.GetSynchronizerID()
	if err != nil {
		return nil, err
	}

	signedHashBytes, err := base64.StdEncoding.DecodeString(signedHash)
	if err != nil {
		return nil, fmt.Errorf("failed to decode signed hash: %w", err)
	}

	onboardingTxs := make([]damlModel.SignedTransaction, len(preparedParty.TopologyTransactions))
	for i, tx := range preparedParty.TopologyTransactions {
		txBytes, err := base64.StdEncoding.DecodeString(tx)
		if err != nil {
			return nil, fmt.Errorf("failed to decode transaction %d: %w", i, err)
		}
		onboardingTxs[i] = damlModel.SignedTransaction{
			Transaction: txBytes,
			Signatures: []damlModel.Signature{
				{
					Format:               damlModel.SignatureFormatConcat,
					Signature:            signedHashBytes,
					SignedBy:             preparedParty.PublicKeyFingerprint,
					SigningAlgorithmSpec: damlModel.SigningAlgorithmSpecED25519,
				},
			},
		}
	}

	multiHashSigs := []damlModel.Signature{
		{
			Format:               damlModel.SignatureFormatConcat,
			Signature:            signedHashBytes,
			SignedBy:             preparedParty.PublicKeyFingerprint,
			SigningAlgorithmSpec: damlModel.SigningAlgorithmSpecED25519,
		},
	}

	partyID, err := l.damlClient.PartyMng.AllocateExternalParty(
		ctx,
		string(syncID),
		onboardingTxs,
		multiHashSigs,
		"",
	)
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
		EventFormat: &damlModel.EventFormat{
			Verbose:        true,
			FiltersByParty: damlFilter.FiltersByParty,
		},
	}

	var activeContracts []*model.ActiveContract

	stream, errChan := l.damlClient.StateService.GetActiveContracts(ctx, req)

	for {
		select {
		case resp, ok := <-stream:
			if !ok {
				return activeContracts, nil
			}
			if entry, ok := resp.ContractEntry.(*damlModel.ActiveContractEntry); ok {
				if entry.ActiveContract != nil && entry.ActiveContract.CreatedEvent != nil {
					ac := entry.ActiveContract.CreatedEvent
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

func (l *LedgerController) ListWallets(ctx context.Context) ([]model.PartyID, error) {
	resp, err := l.damlClient.PartyMng.ListKnownParties(ctx, "", 1000, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list known parties: %w", err)
	}

	wallets := make([]model.PartyID, 0, len(resp.PartyDetails))
	for _, party := range resp.PartyDetails {
		wallets = append(wallets, model.PartyID(party.Party))
	}

	return wallets, nil
}

func (l *LedgerController) GetParticipantID(ctx context.Context) (string, error) {
	participantID, err := l.damlClient.PartyMng.GetParticipantID(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get participant ID: %w", err)
	}

	return participantID, nil
}

func (l *LedgerController) CreatePingCommand(partyID model.PartyID) *model.WrappedCommand {
	return &model.WrappedCommand{
		CreateCommand: &model.CreateCommand{
			TemplateID: "#splice-wallet:Splice.Wallet:Ping",
			CreateArguments: map[string]interface{}{
				"party": string(partyID),
			},
		},
	}
}

func (l *LedgerController) CreateDelegateProxyCommand(ctx context.Context, exchangeParty model.PartyID, treasuryParty model.PartyID) (*model.WrappedCommand, error) {
	partyID, err := l.GetPartyID()
	if err != nil {
		return nil, err
	}

	return &model.WrappedCommand{
		CreateCommand: &model.CreateCommand{
			TemplateID: "#splice-wallet:Splice.Wallet:DelegateProxyProposal",
			CreateArguments: map[string]interface{}{
				"owner":    string(partyID),
				"exchange": string(exchangeParty),
				"treasury": string(treasuryParty),
			},
		},
	}, nil
}

func (l *LedgerController) GrantRights(ctx context.Context, readAsRights []model.PartyID, actAsRights []model.PartyID) error {
	rights := make([]*damlModel.Right, 0, len(readAsRights)+len(actAsRights))

	for _, party := range readAsRights {
		rights = append(rights, &damlModel.Right{
			Type: damlModel.CanReadAs{Party: string(party)},
		})
	}

	for _, party := range actAsRights {
		rights = append(rights, &damlModel.Right{
			Type: damlModel.CanActAs{Party: string(party)},
		})
	}

	_, err := l.damlClient.UserMng.GrantUserRights(ctx, l.userID, "", rights)
	if err != nil {
		return fmt.Errorf("failed to grant user rights: %w", err)
	}

	l.logger.Info().
		Str("userID", l.userID).
		Int("readAsCount", len(readAsRights)).
		Int("actAsCount", len(actAsRights)).
		Msg("Granted user rights")

	return nil
}

func (l *LedgerController) GrantMasterUserRights(ctx context.Context, userID string, canReadAsAnyParty bool, canExecuteAsAnyParty bool) error {
	rights := make([]*damlModel.Right, 0)

	if canReadAsAnyParty {
		rights = append(rights, &damlModel.Right{
			Type: damlModel.ParticipantAdmin{},
		})
	}

	if canExecuteAsAnyParty {
		rights = append(rights, &damlModel.Right{
			Type: damlModel.ParticipantAdmin{},
		})
	}

	_, err := l.damlClient.UserMng.GrantUserRights(ctx, userID, "", rights)
	if err != nil {
		return fmt.Errorf("failed to grant master user rights: %w", err)
	}

	l.logger.Info().
		Str("userID", userID).
		Bool("canReadAsAnyParty", canReadAsAnyParty).
		Bool("canExecuteAsAnyParty", canExecuteAsAnyParty).
		Msg("Granted master user rights")

	return nil
}

func (l *LedgerController) CreateUser(ctx context.Context, userID string, primaryParty model.PartyID) error {
	rights := []*damlModel.Right{
		{Type: damlModel.CanActAs{Party: string(primaryParty)}},
		{Type: damlModel.CanReadAs{Party: string(primaryParty)}},
	}

	user := &damlModel.User{
		ID:           userID,
		PrimaryParty: string(primaryParty),
	}

	_, err := l.damlClient.UserMng.CreateUser(ctx, user, rights)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	l.logger.Info().
		Str("userID", userID).
		Str("primaryParty", string(primaryParty)).
		Msg("Created user")

	return nil
}

func (l *LedgerController) UploadDar(ctx context.Context, darBytes []byte) error {
	err := l.damlClient.PackageMng.UploadDarFile(ctx, darBytes, "")
	if err != nil {
		return fmt.Errorf("failed to upload DAR: %w", err)
	}

	l.logger.Info().Int("bytes", len(darBytes)).Msg("Uploaded DAR file")

	return nil
}

func (l *LedgerController) ListKnownPackages(ctx context.Context) ([]*damlModel.PackageDetails, error) {
	packages, err := l.damlClient.PackageMng.ListKnownPackages(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list known packages: %w", err)
	}
	return packages, nil
}

func (l *LedgerController) IsPackageUploaded(ctx context.Context, packageID string) (bool, error) {
	req := &damlModel.ListPackagesRequest{}
	resp, err := l.damlClient.PackageService.ListPackages(ctx, req)
	if err != nil {
		return false, fmt.Errorf("failed to list packages: %w", err)
	}

	for _, pkg := range resp.PackageIDs {
		if pkg == packageID {
			return true, nil
		}
	}

	return false, nil
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
