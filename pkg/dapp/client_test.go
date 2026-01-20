package dapp

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/noders-team/go-daml/pkg/client"
	damlModel "github.com/noders-team/go-daml/pkg/model"
	"github.com/noders-team/go-wallet-daml/pkg/auth"
	"github.com/noders-team/go-wallet-daml/pkg/controller"
	"github.com/noders-team/go-wallet-daml/pkg/crypto"
	"github.com/noders-team/go-wallet-daml/pkg/model"
	"github.com/noders-team/go-wallet-daml/pkg/sdk"
	"github.com/noders-team/go-wallet-daml/pkg/testutil"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type DappClientTestSuite struct {
	suite.Suite
	ctx          context.Context
	cancel       context.CancelFunc
	walletSDK    *sdk.WalletSDK
	dappClient   *DappClient
	grpcAddr     string
	scanProxyURL string
}

func (s *DappClientTestSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)

	s.grpcAddr = testutil.GetGrpcAddr()
	s.scanProxyURL = testutil.GetScanProxyBaseURL()

	s.walletSDK = sdk.NewWalletSDK()
	s.walletSDK.Configure(sdk.Config{
		AuthFactory: func() auth.AuthController {
			return auth.NewMockAuthController("app-provider")
		},
		LedgerFactory: func(userID string, provider *auth.AuthTokenProvider, isAdmin bool) (*controller.LedgerController, error) {
			return controller.NewLedgerController(
				userID,
				s.grpcAddr,
				s.scanProxyURL,
				provider,
				isAdmin,
			)
		},
		TokenStandardFactory: func(userID string, provider *auth.AuthTokenProvider, isAdmin bool) (*controller.TokenStandardController, error) {
			return controller.NewTokenStandardController(
				userID,
				s.grpcAddr,
				provider,
				isAdmin,
			)
		},
		ValidatorFactory: func(userID string, provider *auth.AuthTokenProvider) (*controller.ValidatorController, error) {
			return controller.NewValidatorController(
				userID,
				s.grpcAddr,
				s.scanProxyURL,
				provider,
			)
		},
	})

	err := s.walletSDK.Connect(s.ctx)
	require.NoError(s.T(), err)

	s.dappClient = NewDappClient(s.walletSDK, s.scanProxyURL)
	require.NotNil(s.T(), s.dappClient)
}

func (s *DappClientTestSuite) TearDownSuite() {
	if s.cancel != nil {
		s.cancel()
	}
}

func TestDappClientTestSuite(t *testing.T) {
	suite.Run(t, new(DappClientTestSuite))
}

func (s *DappClientTestSuite) TestDarsAvailable() {
	dars, err := s.dappClient.DarsAvailable(s.ctx)
	require.NoError(s.T(), err)
	require.NotEmpty(s.T(), dars)

	foundSpliceAmulet := false
	for _, dar := range dars {
		if dar == "splice-amulet" {
			foundSpliceAmulet = true
			break
		}
	}
	require.True(s.T(), foundSpliceAmulet, "splice-amulet package should be available")
}

func (s *DappClientTestSuite) TestRequestAccounts() {
	wallets, err := s.dappClient.RequestAccounts(s.ctx)
	require.NoError(s.T(), err)
	require.NotEmpty(s.T(), wallets)

	require.True(s.T(), len(wallets) >= 1, "there should be at least one wallet account")
	for _, wallet := range wallets {
		require.NotEmpty(s.T(), wallet.Address, "wallet address should not be empty")
		require.NotEmpty(s.T(), wallet.NetworkID, "wallet networkID should not be empty")
		require.Equal(s.T(), "local", wallet.SigningProvider, "wallet signing provider should be 'local'")
		require.Contains(s.T(), wallet.NetworkID, "canton:", "networkID should be in canton format")
	}
}

func (s *DappClientTestSuite) TestPrepareReturn() {
	cl := testutil.GetClient()
	dsoPartyID := testutil.GetDsoPartyID()
	synchronizerID := testutil.GetSynchronizerID()

	receiverPartyID, err := allocatePartyWithCrypto(s.ctx, cl, "receiver")
	require.NoError(s.T(), err)

	_, err = cl.UserMng.GrantUserRights(s.ctx, "app-provider", "", []*damlModel.Right{
		{Type: damlModel.CanActAs{Party: receiverPartyID}},
		{Type: damlModel.CanReadAs{Party: receiverPartyID}},
	})
	require.NoError(s.T(), err)

	s.walletSDK.TokenStandard().SetPartyID(dsoPartyID)
	s.walletSDK.TokenStandard().SetSynchronizerID(synchronizerID)

	mintAmount := decimal.NewFromFloat(100.0)
	_, err = s.walletSDK.TokenStandard().CreateAndSubmitTapInternal(s.ctx, dsoPartyID, mintAmount, "", string(dsoPartyID))
	require.NoError(s.T(), err)

	time.Sleep(5 * time.Second)

	err = s.walletSDK.SetPartyID(s.ctx, dsoPartyID, &synchronizerID)
	require.NoError(s.T(), err)

	holdings, err := s.walletSDK.TokenStandard().ListHoldingUtxos(s.ctx, false, 10)
	require.NoError(s.T(), err)
	require.NotEmpty(s.T(), holdings, "should have holdings after mint")

	transferAmount := decimal.NewFromFloat(10.0)
	transferResult, err := s.walletSDK.TokenStandard().CreateTransfer(
		s.ctx,
		dsoPartyID,
		model.PartyID(receiverPartyID),
		transferAmount,
		holdings[0].InstrumentID,
		holdings[0].InstrumentAdmin,
		[]string{holdings[0].ContractID},
		"test-dapp-transfer",
	)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), transferResult)

	req := &model.JsPrepareSubmissionRequest{
		Commands: []*damlModel.Command{transferResult.Command},
		ActAs:    []string{string(dsoPartyID), receiverPartyID},
	}

	resp, err := s.dappClient.PrepareReturn(s.ctx, req)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), resp)
	require.NotEmpty(s.T(), resp.PreparedTransaction, "prepared transaction should not be empty")
	require.NotEmpty(s.T(), resp.PreparedTransactionHash, "prepared transaction hash should not be empty")
}

func (s *DappClientTestSuite) TestPrepareExecute() {
	cl := testutil.GetClient()
	dsoPartyID := testutil.GetDsoPartyID()
	synchronizerID := testutil.GetSynchronizerID()

	receiverPartyID, err := allocatePartyWithCrypto(s.ctx, cl, "receiver")
	require.NoError(s.T(), err)

	_, err = cl.UserMng.GrantUserRights(s.ctx, "app-provider", "", []*damlModel.Right{
		{Type: damlModel.CanActAs{Party: receiverPartyID}},
		{Type: damlModel.CanReadAs{Party: receiverPartyID}},
	})
	require.NoError(s.T(), err)

	s.walletSDK.TokenStandard().SetPartyID(dsoPartyID)
	s.walletSDK.TokenStandard().SetSynchronizerID(synchronizerID)

	mintAmount := decimal.NewFromFloat(100.0)
	_, err = s.walletSDK.TokenStandard().CreateAndSubmitTapInternal(s.ctx, dsoPartyID, mintAmount, "", string(dsoPartyID))
	require.NoError(s.T(), err)

	time.Sleep(5 * time.Second)

	err = s.walletSDK.SetPartyID(s.ctx, dsoPartyID, &synchronizerID)
	require.NoError(s.T(), err)

	holdings, err := s.walletSDK.TokenStandard().ListHoldingUtxos(s.ctx, false, 10)
	require.NoError(s.T(), err)
	require.NotEmpty(s.T(), holdings, "should have holdings after mint")

	transferAmount := decimal.NewFromFloat(10.0)
	transferResult, err := s.walletSDK.TokenStandard().CreateTransfer(
		s.ctx,
		dsoPartyID,
		model.PartyID(receiverPartyID),
		transferAmount,
		holdings[0].InstrumentID,
		holdings[0].InstrumentAdmin,
		[]string{holdings[0].ContractID},
		"test-dapp-execute",
	)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), transferResult)

	txChan, err := s.dappClient.SubscribeTxChanged(s.ctx)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), txChan)

	commandID := uuid.New().String()
	req := &model.JsPrepareSubmissionRequest{
		CommandID: commandID,
		Commands:  []*damlModel.Command{transferResult.Command},
		ActAs:     []string{string(dsoPartyID), receiverPartyID},
	}

	err = s.dappClient.PrepareExecute(s.ctx, req)
	require.NoError(s.T(), err)

	select {
	case event := <-txChan:
		pendingEvent, ok := event.(*model.TxChangedPendingEvent)
		require.True(s.T(), ok, "event should be TxChangedPendingEvent")
		require.Equal(s.T(), "pending", pendingEvent.Status)
		require.Equal(s.T(), commandID, pendingEvent.CommandID)
	case <-time.After(2 * time.Second):
		s.T().Fatal("timeout waiting for txChanged event")
	}
}

func (s *DappClientTestSuite) TestPrepareExecuteAndWait() {
	cl := testutil.GetClient()
	dsoPartyID := testutil.GetDsoPartyID()
	synchronizerID := testutil.GetSynchronizerID()

	receiverPartyID, err := allocatePartyWithCrypto(s.ctx, cl, "receiver")
	require.NoError(s.T(), err)

	_, err = cl.UserMng.GrantUserRights(s.ctx, "app-provider", "", []*damlModel.Right{
		{Type: damlModel.CanActAs{Party: receiverPartyID}},
		{Type: damlModel.CanReadAs{Party: receiverPartyID}},
	})
	require.NoError(s.T(), err)

	s.walletSDK.TokenStandard().SetPartyID(dsoPartyID)
	s.walletSDK.TokenStandard().SetSynchronizerID(synchronizerID)

	mintAmount := decimal.NewFromFloat(100.0)
	_, err = s.walletSDK.TokenStandard().CreateAndSubmitTapInternal(s.ctx, dsoPartyID, mintAmount, "", string(dsoPartyID))
	require.NoError(s.T(), err)

	time.Sleep(5 * time.Second)

	err = s.walletSDK.SetPartyID(s.ctx, dsoPartyID, &synchronizerID)
	require.NoError(s.T(), err)

	holdings, err := s.walletSDK.TokenStandard().ListHoldingUtxos(s.ctx, false, 10)
	require.NoError(s.T(), err)
	require.NotEmpty(s.T(), holdings, "should have holdings after mint")

	transferAmount := decimal.NewFromFloat(10.0)
	transferResult, err := s.walletSDK.TokenStandard().CreateTransfer(
		s.ctx,
		dsoPartyID,
		model.PartyID(receiverPartyID),
		transferAmount,
		holdings[0].InstrumentID,
		holdings[0].InstrumentAdmin,
		[]string{holdings[0].ContractID},
		"test-dapp-execute-wait",
	)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), transferResult)

	req := &model.JsPrepareSubmissionRequest{
		Commands: []*damlModel.Command{transferResult.Command},
		ActAs:    []string{string(dsoPartyID), receiverPartyID},
	}

	result, err := s.dappClient.PrepareExecuteAndWait(s.ctx, req)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), result)
	require.Equal(s.T(), "executed", result.Status)
	require.NotEmpty(s.T(), result.CommandID, "command ID should not be empty")
	require.NotNil(s.T(), result.Payload)
	require.NotEmpty(s.T(), result.Payload.UpdateID, "update ID should not be empty")
}

func allocatePartyWithCrypto(ctx context.Context, cl *client.DamlBindingClient, displayName string) (string, error) {
	keyPair, err := crypto.CreateKeyPair()
	if err != nil {
		return "", err
	}

	_, err = crypto.CreateFingerprintFromKey(keyPair.PublicKey)
	if err != nil {
		return "", err
	}

	partyHint := fmt.Sprintf("%s-%s", displayName, uuid.New().String()[:8])

	partyDetails, err := cl.PartyMng.AllocateParty(ctx, partyHint, nil, "")
	if err != nil {
		return "", err
	}

	time.Sleep(2 * time.Second)

	return partyDetails.Party, nil
}
