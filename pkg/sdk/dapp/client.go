package dapp

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/noders-team/go-wallet-daml/pkg/model"
	"github.com/noders-team/go-wallet-daml/pkg/sdk"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type DappClient struct {
	sdk         *sdk.WalletSDK
	httpBaseURL string
	httpClient  *http.Client
	emitter     *EventEmitter
	txCache     map[string]*model.PrepareSubmissionResponse
	mu          sync.RWMutex
	logger      zerolog.Logger
}

func NewDappClient(walletSDK *sdk.WalletSDK, httpBaseURL string) *DappClient {
	logger := log.Logger.With().Str("component", "dapp-client").Logger()

	return &DappClient{
		sdk:         walletSDK,
		httpBaseURL: httpBaseURL,
		httpClient:  &http.Client{},
		emitter:     NewEventEmitter(),
		txCache:     make(map[string]*model.PrepareSubmissionResponse),
		logger:      logger,
	}
}

func (d *DappClient) Status(ctx context.Context) (*StatusEvent, error) {
	status := &StatusEvent{
		Kernel: &KernelInfo{
			ID:         uuid.New().String(),
			ClientType: "desktop",
		},
		IsConnected:        false,
		IsNetworkConnected: false,
	}

	if d.sdk.Auth() == nil {
		status.NetworkReason = "auth controller not configured"
		return status, nil
	}

	authCtx, err := d.sdk.Auth().GetUserToken(ctx)
	if err != nil {
		status.NetworkReason = fmt.Sprintf("not authenticated: %v", err)
		return status, nil
	}

	status.IsConnected = true
	status.Session = &SessionInfo{
		UserID:      authCtx.UserID,
		AccessToken: authCtx.AccessToken,
	}

	if d.sdk.UserLedger() == nil {
		status.NetworkReason = "user ledger not initialized"
		return status, nil
	}

	partyID, err := d.sdk.UserLedger().GetPartyID()
	if err != nil {
		status.NetworkReason = fmt.Sprintf("party ID not set: %v", err)
		return status, nil
	}

	syncResp, err := d.sdk.UserLedger().ListSynchronizers(ctx, partyID)
	if err != nil {
		status.NetworkReason = fmt.Sprintf("failed to list synchronizers: %v", err)
		return status, nil
	}

	if len(syncResp.ConnectedSynchronizers) == 0 {
		status.NetworkReason = "no synchronizers connected"
		return status, nil
	}

	status.IsNetworkConnected = true
	status.Network = &NetworkInfo{
		NetworkID: fmt.Sprintf("canton:%s", syncResp.ConnectedSynchronizers[0].SynchronizerID),
		LedgerApi: &LedgerApiInfo{
			BaseURL: d.httpBaseURL,
		},
	}

	return status, nil
}

func (d *DappClient) Connect(ctx context.Context) (*StatusEvent, error) {
	if d.sdk.Auth() == nil {
		return nil, ErrNotAuthenticated
	}

	_, err := d.sdk.Auth().GetUserToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate: %w", err)
	}

	if d.sdk.UserLedger() != nil {
		if err := d.sdk.UserLedger().AwaitInit(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize ledger: %w", err)
		}
	}

	return d.Status(ctx)
}

func (d *DappClient) Disconnect(ctx context.Context) error {
	d.emitter.Close()

	d.mu.Lock()
	d.txCache = make(map[string]*model.PrepareSubmissionResponse)
	d.mu.Unlock()

	return nil
}

func (d *DappClient) DarsAvailable(ctx context.Context) ([]string, error) {
	if d.sdk.UserLedger() == nil {
		return nil, ErrNoUserLedger
	}

	packages, err := d.sdk.UserLedger().ListKnownPackages(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list packages: %w", err)
	}

	dars := make([]string, 0, len(packages))
	for _, pkg := range packages {
		if pkg.Name != "" {
			dars = append(dars, pkg.Name)
		}
	}

	return dars, nil
}

func (d *DappClient) PrepareReturn(ctx context.Context, req *JsPrepareSubmissionRequest) (*JsPrepareSubmissionResponse, error) {
	if d.sdk.UserLedger() == nil {
		return nil, ErrNoUserLedger
	}

	commands, err := d.convertToInternalCommands(req.Commands)
	if err != nil {
		return nil, fmt.Errorf("failed to convert commands: %w", err)
	}

	disclosed := d.convertDisclosedContracts(req.DisclosedContracts)

	commandID := req.CommandID
	if commandID == "" {
		commandID = uuid.New().String()
	}

	prepared, err := d.sdk.UserLedger().PrepareSubmission(ctx, commands, commandID, disclosed)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare submission: %w", err)
	}

	return &JsPrepareSubmissionResponse{
		PreparedTransaction:     base64.StdEncoding.EncodeToString(prepared.PreparedTransaction),
		PreparedTransactionHash: prepared.PreparedTransactionHash,
	}, nil
}

func (d *DappClient) PrepareExecute(ctx context.Context, req *JsPrepareSubmissionRequest) error {
	if d.sdk.UserLedger() == nil {
		return ErrNoUserLedger
	}

	commands, err := d.convertToInternalCommands(req.Commands)
	if err != nil {
		return fmt.Errorf("failed to convert commands: %w", err)
	}

	disclosed := d.convertDisclosedContracts(req.DisclosedContracts)

	commandID := req.CommandID
	if commandID == "" {
		commandID = uuid.New().String()
	}

	prepared, err := d.sdk.UserLedger().PrepareSubmission(ctx, commands, commandID, disclosed)
	if err != nil {
		return fmt.Errorf("failed to prepare submission: %w", err)
	}

	d.mu.Lock()
	d.txCache[commandID] = prepared
	d.mu.Unlock()

	d.emitter.EmitTxChanged(&TxChangedPendingEvent{
		Status:    "pending",
		CommandID: commandID,
	})

	return nil
}

func (d *DappClient) PrepareExecuteAndWait(ctx context.Context, req *JsPrepareSubmissionRequest) (*TxChangedExecutedEvent, error) {
	if d.sdk.UserLedger() == nil {
		return nil, ErrNoUserLedger
	}

	commands, err := d.convertToInternalCommands(req.Commands)
	if err != nil {
		return nil, fmt.Errorf("failed to convert commands: %w", err)
	}

	disclosed := d.convertDisclosedContracts(req.DisclosedContracts)

	commandID := req.CommandID
	if commandID == "" {
		commandID = uuid.New().String()
	}

	_, err = d.sdk.UserLedger().PrepareSubmission(ctx, commands, commandID, disclosed)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare submission: %w", err)
	}

	d.emitter.EmitTxChanged(&TxChangedPendingEvent{
		Status:    "pending",
		CommandID: commandID,
	})

	return nil, ErrSignatureRequired
}

func (d *DappClient) LedgerApi(ctx context.Context, req *LedgerApiRequest) (*LedgerApiResult, error) {
	if d.sdk.AuthTokenProvider() == nil {
		return nil, ErrNotAuthenticated
	}

	token, err := d.sdk.AuthTokenProvider().GetUserAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	url := d.httpBaseURL + req.Resource
	httpReq, err := http.NewRequestWithContext(ctx, req.RequestMethod, url, strings.NewReader(req.Body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return &LedgerApiResult{
		Response: string(body),
	}, nil
}

func (d *DappClient) RequestAccounts(ctx context.Context) ([]*Wallet, error) {
	if d.sdk.UserLedger() == nil {
		return nil, ErrNoUserLedger
	}

	parties, err := d.sdk.UserLedger().ListWallets(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list wallets: %w", err)
	}

	wallets := make([]*Wallet, 0, len(parties))
	for _, party := range parties {
		syncResp, err := d.sdk.UserLedger().ListSynchronizers(ctx, party)
		if err != nil {
			continue
		}

		networkID := ""
		if len(syncResp.ConnectedSynchronizers) > 0 {
			networkID = fmt.Sprintf("canton:%s", syncResp.ConnectedSynchronizers[0].SynchronizerID)
		}

		wallets = append(wallets, &Wallet{
			Address:         string(party),
			NetworkID:       networkID,
			SigningProvider: "local",
		})
	}

	return wallets, nil
}

func (d *DappClient) SubscribeAccountsChanged(ctx context.Context) (<-chan []*Wallet, error) {
	ch := make(chan []*Wallet, 10)
	d.emitter.AddAccountsListener(ch)
	return ch, nil
}

func (d *DappClient) SubscribeTxChanged(ctx context.Context) (<-chan interface{}, error) {
	ch := make(chan interface{}, 10)
	d.emitter.AddTxListener(ch)
	return ch, nil
}

func (d *DappClient) convertToInternalCommands(commands interface{}) (interface{}, error) {
	return commands, nil
}

func (d *DappClient) convertDisclosedContracts(contracts []*DisclosedContract) []*model.DisclosedContract {
	if contracts == nil {
		return nil
	}

	result := make([]*model.DisclosedContract, len(contracts))
	for i, c := range contracts {
		blobBytes, _ := base64.StdEncoding.DecodeString(c.CreatedEventBlob)
		result[i] = &model.DisclosedContract{
			TemplateID:       c.TemplateID,
			ContractID:       c.ContractID,
			CreatedEventBlob: blobBytes,
			SynchronizerID:   c.SynchronizerID,
		}
	}
	return result
}
