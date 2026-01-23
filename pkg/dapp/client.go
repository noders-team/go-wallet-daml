package dapp

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"

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
	logger      zerolog.Logger
}

func NewDappClient(walletSDK *sdk.WalletSDK, httpBaseURL string) *DappClient {
	logger := log.Logger.With().Str("component", "dapp-client").Logger()

	return &DappClient{
		sdk:         walletSDK,
		httpBaseURL: httpBaseURL,
		httpClient:  &http.Client{},
		emitter:     NewEventEmitter(),
		logger:      logger,
	}
}

func (d *DappClient) Status(ctx context.Context) (*model.StatusEvent, error) {
	status := &model.StatusEvent{
		Kernel: &model.KernelInfo{
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
	status.Session = &model.SessionInfo{
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
	status.Network = &model.NetworkInfo{
		NetworkID: fmt.Sprintf("canton:%s", syncResp.ConnectedSynchronizers[0].SynchronizerID),
		LedgerApi: &model.LedgerApiInfo{
			BaseURL: d.httpBaseURL,
		},
	}

	return status, nil
}

func (d *DappClient) Connect(ctx context.Context) (*model.StatusEvent, error) {
	if d.sdk.Auth() == nil {
		return nil, model.ErrNotAuthenticated
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
	return nil
}

func (d *DappClient) DarsAvailable(ctx context.Context) ([]string, error) {
	if d.sdk.UserLedger() == nil {
		return nil, model.ErrNoUserLedger
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

func (d *DappClient) PrepareReturn(ctx context.Context, req *model.JsPrepareSubmissionRequest) (*model.JsPrepareSubmissionResponse, error) {
	if d.sdk.UserLedger() == nil {
		return nil, model.ErrNoUserLedger
	}

	commandID := d.commandIDOrNew(req.CommandID)
	prepared, err := d.sdk.UserLedger().PrepareSubmission(ctx, req.Commands,
		commandID, req.DisclosedContracts, req.ActAs, req.ReadAs)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare submission: %w", err)
	}

	return &model.JsPrepareSubmissionResponse{
		PreparedTransaction:     base64.StdEncoding.EncodeToString(prepared.PreparedTransaction),
		PreparedTransactionHash: prepared.PreparedTransactionHash,
	}, nil
}

func (d *DappClient) PrepareExecute(ctx context.Context, req *model.JsPrepareSubmissionRequest) error {
	if d.sdk.UserLedger() == nil {
		return model.ErrNoUserLedger
	}

	commandID := d.commandIDOrNew(req.CommandID)
	_, err := d.sdk.UserLedger().PrepareSubmission(ctx, req.Commands, commandID,
		req.DisclosedContracts, req.ActAs, req.ReadAs)
	if err != nil {
		return fmt.Errorf("failed to prepare submission: %w", err)
	}

	d.emitter.EmitTxChanged(&model.TxChangedPendingEvent{
		Status:    "pending",
		CommandID: commandID,
	})

	return nil
}

func (d *DappClient) PrepareExecuteAndWait(ctx context.Context, req *model.JsPrepareSubmissionRequest) (*model.TxChangedExecutedEvent, error) {
	if d.sdk.UserLedger() == nil {
		return nil, model.ErrNoUserLedger
	}

	commandID := d.commandIDOrNew(req.CommandID)

	d.emitter.EmitTxChanged(&model.TxChangedPendingEvent{
		Status:    "pending",
		CommandID: commandID,
	})

	result, err := d.sdk.UserLedger().SubmitCommandAndWait(ctx,
		req.Commands, commandID,
		req.DisclosedContracts, req.ActAs, req.ReadAs)
	if err != nil {
		d.emitter.EmitTxChanged(&model.TxChangedFailedEvent{
			Status:    "failed",
			CommandID: commandID,
		})
		return nil, fmt.Errorf("failed to submit command: %w", err)
	}

	event := &model.TxChangedExecutedEvent{
		Status:    "executed",
		CommandID: commandID,
		Payload: &model.TxChangedExecutedPayload{
			UpdateID:         result.UpdateID,
			CompletionOffset: result.CompletionOffset,
		},
	}

	d.emitter.EmitTxChanged(event)

	return event, nil
}

func (d *DappClient) LedgerApi(ctx context.Context, req *model.LedgerApiRequest) (*model.LedgerApiResult, error) {
	if d.sdk.AuthTokenProvider() == nil {
		return nil, model.ErrNotAuthenticated
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

	return &model.LedgerApiResult{
		Response: string(body),
	}, nil
}

func (d *DappClient) RequestAccounts(ctx context.Context) ([]*model.Wallet, error) {
	if d.sdk.UserLedger() == nil {
		return nil, model.ErrNoUserLedger
	}

	parties, err := d.sdk.UserLedger().ListWallets(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list wallets: %w", err)
	}

	wallets := make([]*model.Wallet, 0, len(parties))
	for _, party := range parties {
		syncResp, err := d.sdk.UserLedger().ListSynchronizers(ctx, party)
		if err != nil {
			continue
		}

		networkID := ""
		if len(syncResp.ConnectedSynchronizers) > 0 {
			networkID = fmt.Sprintf("canton:%s", syncResp.ConnectedSynchronizers[0].SynchronizerID)
		}

		wallets = append(wallets, &model.Wallet{
			Address:         string(party),
			NetworkID:       networkID,
			SigningProvider: "local",
		})
	}

	return wallets, nil
}

func (d *DappClient) SubscribeAccountsChanged(_ context.Context) (<-chan []*model.Wallet, error) {
	ch := make(chan []*model.Wallet, 10)
	d.emitter.AddAccountsListener(ch)
	return ch, nil
}

func (d *DappClient) SubscribeTxChanged(_ context.Context) (<-chan interface{}, error) {
	ch := make(chan interface{}, 10)
	d.emitter.AddTxListener(ch)
	return ch, nil
}

func (d *DappClient) commandIDOrNew(commandID string) string {
	if commandID == "" {
		return uuid.New().String()
	}
	return commandID
}
