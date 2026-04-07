package dapp

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
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
	"github.com/noders-team/go-wallet-daml/pkg/wrapper"
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

	authCtrl := auth.NewMockAuthController("app-provider")
	authProvider := auth.NewAuthTokenProvider(authCtrl)

	damlCl, err := client.NewDamlClient("", s.grpcAddr).Build(s.ctx)
	require.NoError(s.T(), err)

	scanProxy := wrapper.NewScanProxyClient(s.scanProxyURL, authProvider, false)

	s.walletSDK.Configure(sdk.Config{
		AuthFactory: func() auth.AuthController {
			return authCtrl
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
		TokenStandardFactory: func(userID string, dc *client.DamlBindingClient) (*controller.TokenStandardController, error) {
			return controller.NewTokenStandardController(userID, dc)
		},
		ValidatorFactory: func(userID string, sp *wrapper.ScanProxyClient, dc *client.DamlBindingClient) (*controller.ValidatorController, error) {
			return controller.NewValidatorController(userID, sp, dc)
		},
		DamlClient: damlCl,
		ScanProxy:  scanProxy,
	})

	err = s.walletSDK.Connect(s.ctx)
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

	receiverPartyID, err := s.allocatePartyWithRights(s.ctx, cl, "receiver")
	require.NoError(s.T(), err)

	holdings, err := s.mintAndAwaitHoldings(s.ctx, dsoPartyID, synchronizerID, 2, 5*time.Second)
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

	receiverPartyID, err := s.allocatePartyWithRights(s.ctx, cl, "receiver")
	require.NoError(s.T(), err)

	holdings, err := s.mintAndAwaitHoldings(s.ctx, dsoPartyID, synchronizerID, 2, 5*time.Second)
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

	receiverPartyID, err := s.allocatePartyWithRights(s.ctx, cl, "receiver")
	require.NoError(s.T(), err)

	holdings, err := s.mintAndAwaitHoldings(s.ctx, dsoPartyID, synchronizerID, 2, 5*time.Second)
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

func (s *DappClientTestSuite) TestPrepareReturnWithExternalParty() {
	cl := testutil.GetClient()
	dsoPartyID := testutil.GetDsoPartyID()
	synchronizerID := testutil.GetSynchronizerID()

	externalObserverID, err := s.allocateExternalPartyWithRead(s.ctx, cl, "ext-observer")
	require.NoError(s.T(), err)

	holdings, err := s.mintAndAwaitHoldings(s.ctx, dsoPartyID, synchronizerID, 6, 3*time.Second)
	require.NoError(s.T(), err)
	require.NotEmpty(s.T(), holdings, "should have holdings after mint")

	transferAmount := decimal.NewFromFloat(10.0)
	transferResult, err := s.walletSDK.TokenStandard().CreateTransfer(
		s.ctx,
		dsoPartyID,
		dsoPartyID,
		transferAmount,
		holdings[0].InstrumentID,
		holdings[0].InstrumentAdmin,
		[]string{holdings[0].ContractID},
		"test-dapp-ext-prepare-return",
	)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), transferResult)

	req := &model.JsPrepareSubmissionRequest{
		Commands: []*damlModel.Command{transferResult.Command},
		ActAs:    []string{string(dsoPartyID)},
		ReadAs:   []string{externalObserverID},
	}

	resp, err := s.dappClient.PrepareReturn(s.ctx, req)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), resp)
	require.NotEmpty(s.T(), resp.PreparedTransaction, "prepared transaction should not be empty")
	require.NotEmpty(s.T(), resp.PreparedTransactionHash, "prepared transaction hash should not be empty")
}

func (s *DappClientTestSuite) TestPrepareExecuteWithExternalParty() {
	cl := testutil.GetClient()
	dsoPartyID := testutil.GetDsoPartyID()
	synchronizerID := testutil.GetSynchronizerID()

	receiverPartyID, err := s.allocatePartyWithRights(s.ctx, cl, "receiver")
	require.NoError(s.T(), err)

	holdings, err := s.mintAndAwaitHoldings(s.ctx, dsoPartyID, synchronizerID, 2, 5*time.Second)
	require.NoError(s.T(), err)
	require.NotEmpty(s.T(), holdings, "should have holdings after mint")

	s.walletSDK.TokenStandard().SetPartyID(model.PartyID(receiverPartyID))
	s.walletSDK.TokenStandard().SetSynchronizerID(synchronizerID)
	initialBalance, err := s.walletSDK.TokenStandard().ListHoldingUtxos(s.ctx, false, 10)
	require.NoError(s.T(), err)
	require.Empty(s.T(), initialBalance, "receiver party should have no holdings initially")

	s.walletSDK.TokenStandard().SetPartyID(dsoPartyID)
	s.walletSDK.TokenStandard().SetSynchronizerID(synchronizerID)

	amuletRulesContractID := testutil.GetAmuletRulesTemplateID()
	require.NotEmpty(s.T(), amuletRulesContractID, "AmuletRules contract ID should be set")

	resRules := strings.Split(amuletRulesContractID, ":")
	require.NotEmpty(s.T(), resRules, "AmuletRules contract ID should be in format packageID:moduleName:entityName")

	transferPreapprovalTemplateID := resRules[0] + ":Splice.AmuletRules:TransferPreapproval"

	now := time.Now().UTC()
	wrappedCmd := s.walletSDK.UserLedger().CreateTransferPreapprovalCommand(
		string(dsoPartyID),
		receiverPartyID,
		string(dsoPartyID),
		now,
		now,
		now.Add(24*time.Hour),
	)

	damlCmd := wrappedCmd.CreateCommand.ToDamlCreateCommand()
	createCmd, ok := damlCmd.Command.(*damlModel.CreateCommand)
	require.True(s.T(), ok, "command should be CreateCommand")
	createCmd.TemplateID = transferPreapprovalTemplateID

	preapprovalReq := &model.JsPrepareSubmissionRequest{
		Commands: []*damlModel.Command{damlCmd},
		ActAs:    []string{string(dsoPartyID), receiverPartyID},
		ReadAs:   []string{},
	}

	preapprovalResult, err := s.dappClient.PrepareExecuteAndWait(s.ctx, preapprovalReq)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), preapprovalResult)
	require.Equal(s.T(), "executed", preapprovalResult.Status)

	time.Sleep(3 * time.Second)

	transferAmount := decimal.NewFromFloat(10.0)
	transferResult, err := s.walletSDK.TokenStandard().CreateTransfer(
		s.ctx,
		dsoPartyID,
		model.PartyID(receiverPartyID),
		transferAmount,
		holdings[0].InstrumentID,
		holdings[0].InstrumentAdmin,
		[]string{holdings[0].ContractID},
		"test-dapp-ext-execute",
	)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), transferResult)

	req := &model.JsPrepareSubmissionRequest{
		Commands: []*damlModel.Command{transferResult.Command},
		ActAs:    []string{string(dsoPartyID), receiverPartyID},
		ReadAs:   []string{},
	}

	result, err := s.dappClient.PrepareExecuteAndWait(s.ctx, req)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), result)
	require.Equal(s.T(), "executed", result.Status)

	s.walletSDK.TokenStandard().SetPartyID(model.PartyID(receiverPartyID))
	s.walletSDK.TokenStandard().SetSynchronizerID(synchronizerID)

	finalHoldings, err := s.walletSDK.TokenStandard().ListHoldingUtxos(s.ctx, false, 10)
	require.NoError(s.T(), err)
	require.NotEmpty(s.T(), finalHoldings, "receiver party should have holdings after transfer")

	totalBalance := decimal.Zero
	for _, holding := range finalHoldings {
		totalBalance = totalBalance.Add(holding.Amount)
	}
	require.True(s.T(), totalBalance.GreaterThanOrEqual(transferAmount), "receiver party balance should be at least %s, got %s", transferAmount.String(), totalBalance.String())
}

func (s *DappClientTestSuite) TestPrepareExecuteAndWaitWithExternalParty() {
	cl := testutil.GetClient()
	dsoPartyID := testutil.GetDsoPartyID()
	synchronizerID := testutil.GetSynchronizerID()

	externalObserverID, err := s.allocateExternalPartyWithRead(s.ctx, cl, "ext-observer")
	require.NoError(s.T(), err)

	holdings, err := s.mintAndAwaitHoldings(s.ctx, dsoPartyID, synchronizerID, 2, 5*time.Second)
	require.NoError(s.T(), err)
	require.NotEmpty(s.T(), holdings, "should have holdings after mint")

	transferAmount := decimal.NewFromFloat(10.0)
	transferResult, err := s.walletSDK.TokenStandard().CreateTransfer(
		s.ctx,
		dsoPartyID,
		dsoPartyID,
		transferAmount,
		holdings[0].InstrumentID,
		holdings[0].InstrumentAdmin,
		[]string{holdings[0].ContractID},
		"test-dapp-ext-execute-wait",
	)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), transferResult)

	req := &model.JsPrepareSubmissionRequest{
		Commands: []*damlModel.Command{transferResult.Command},
		ActAs:    []string{string(dsoPartyID)},
		ReadAs:   []string{externalObserverID},
	}

	result, err := s.dappClient.PrepareExecuteAndWait(s.ctx, req)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), result)
	require.Equal(s.T(), "executed", result.Status)
	require.NotEmpty(s.T(), result.CommandID, "command ID should not be empty")
	require.NotNil(s.T(), result.Payload)
	require.NotEmpty(s.T(), result.Payload.UpdateID, "update ID should not be empty")
}

func (s *DappClientTestSuite) allocatePartyWithRights(ctx context.Context, cl *client.DamlBindingClient, displayName string) (string, error) {
	partyID, err := allocatePartyWithCrypto(ctx, cl, displayName)
	if err != nil {
		return "", err
	}
	err = s.grantUserRights(ctx, cl, []*damlModel.Right{
		{Type: damlModel.CanActAs{Party: partyID}},
		{Type: damlModel.CanReadAs{Party: partyID}},
	})
	if err != nil {
		return "", err
	}
	return partyID, nil
}

func (s *DappClientTestSuite) allocateExternalPartyWithRead(ctx context.Context, cl *client.DamlBindingClient, displayName string) (string, error) {
	partyID, err := allocateExternalPartyWithCrypto(ctx, cl, displayName)
	if err != nil {
		return "", err
	}
	err = s.grantUserRights(ctx, cl, []*damlModel.Right{
		{Type: damlModel.CanReadAs{Party: partyID}},
	})
	if err != nil {
		return "", err
	}
	return partyID, nil
}

func (s *DappClientTestSuite) grantUserRights(ctx context.Context, cl *client.DamlBindingClient, rights []*damlModel.Right) error {
	_, err := cl.UserMng.GrantUserRights(ctx, "app-provider", "", rights)
	return err
}

func (s *DappClientTestSuite) mintAndAwaitHoldings(ctx context.Context, dsoPartyID, synchronizerID model.PartyID, retries int, delay time.Duration) ([]*model.HoldingUTXO, error) {
	s.walletSDK.TokenStandard().SetPartyID(dsoPartyID)
	s.walletSDK.TokenStandard().SetSynchronizerID(synchronizerID)

	mintAmount := decimal.NewFromFloat(100.0)
	_, err := s.walletSDK.TokenStandard().CreateAndSubmitTapInternal(ctx, dsoPartyID, mintAmount, "", string(dsoPartyID))
	if err != nil {
		return nil, err
	}

	err = s.walletSDK.SetPartyID(ctx, dsoPartyID, &synchronizerID)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for i := 0; i < retries; i++ {
		holdings, err := s.walletSDK.TokenStandard().ListHoldingUtxos(ctx, false, 10)
		if err == nil && len(holdings) > 0 {
			return holdings, nil
		}
		if err != nil {
			lastErr = err
		}
		if i < retries-1 {
			time.Sleep(delay)
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no holdings after mint")
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

func allocateExternalPartyWithCrypto(ctx context.Context, cl *client.DamlBindingClient, displayName string) (string, error) {
	syncResp, err := cl.StateService.GetConnectedSynchronizers(ctx, &damlModel.GetConnectedSynchronizersRequest{})
	if err != nil {
		return "", err
	}
	if len(syncResp.ConnectedSynchronizers) == 0 {
		return "", fmt.Errorf("no connected synchronizers")
	}
	synchronizerID := syncResp.ConnectedSynchronizers[0].SynchronizerID

	participantID, err := cl.PartyMng.GetParticipantID(ctx)
	if err != nil {
		return "", err
	}

	keyPair, err := crypto.CreateKeyPair()
	if err != nil {
		return "", err
	}

	keyFingerprint, err := crypto.CreateFingerprintFromKey(keyPair.PublicKey)
	if err != nil {
		return "", err
	}

	namespace := keyFingerprint
	partyID := fmt.Sprintf("%s-%s", displayName, uuid.New().String()[:8])
	fullPartyID := fmt.Sprintf("%s::%s", partyID, namespace)

	onboardingTxs, multiHashSigs, err := createOnboardingTransactions(
		ctx, cl, fullPartyID, keyPair.PublicKey, keyPair.PrivateKey,
		participantID, keyFingerprint,
	)
	if err != nil {
		return "", err
	}

	allocatedPartyID, err := cl.PartyMng.AllocateExternalParty(
		ctx, synchronizerID, onboardingTxs, multiHashSigs, "",
	)
	if err != nil {
		return "", err
	}

	time.Sleep(5 * time.Second)

	return allocatedPartyID, nil
}

func createOnboardingTransactions(
	ctx context.Context,
	cl *client.DamlBindingClient,
	partyID string,
	publicKeyBase64 string,
	privateKeyBase64 string,
	participantID string,
	keyFingerprint string,
) ([]damlModel.SignedTransaction, []damlModel.Signature, error) {
	publicKeyBytes, err := base64.StdEncoding.DecodeString(publicKeyBase64)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode public key: %w", err)
	}

	pubKey := &damlModel.PublicKey{
		Format:  3,
		Key:     publicKeyBytes,
		Scheme:  int32(damlModel.SigningKeySchemeED25519),
		KeySpec: int32(damlModel.SigningKeySpecCurve25519),
		Usage:   []int32{},
	}

	storeID := &damlModel.StoreID{Value: "authorized"}

	proposals := []*damlModel.GenerateTransactionProposal{
		{
			Operation: damlModel.OperationAddReplace,
			Serial:    1,
			Mapping: &damlModel.NamespaceDelegationMapping{
				Namespace:        keyFingerprint,
				TargetKey:        *pubKey,
				IsRootDelegation: true,
			},
			Store: storeID,
		},
		{
			Operation: damlModel.OperationAddReplace,
			Serial:    1,
			Mapping: &damlModel.PartyToKeyMapping{
				Party:       partyID,
				Threshold:   1,
				SigningKeys: []damlModel.PublicKey{*pubKey},
			},
			Store: storeID,
		},
		{
			Operation: damlModel.OperationAddReplace,
			Serial:    1,
			Mapping: &damlModel.PartyToParticipantMapping{
				Party:     partyID,
				Threshold: 1,
				Participants: []damlModel.HostingParticipant{
					{
						ParticipantUID: participantID,
						Permission:     damlModel.ParticipantPermissionConfirmation,
					},
				},
			},
			Store: storeID,
		},
	}

	genResp, err := cl.TopologyManagerWrite.GenerateTransactions(ctx, &damlModel.GenerateTransactionsRequest{
		Proposals: proposals,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate transactions: %w", err)
	}

	onboardingTxs := make([]damlModel.SignedTransaction, len(genResp.GeneratedTransactions))
	transactionHashes := make([][]byte, len(genResp.GeneratedTransactions))

	for i, genTx := range genResp.GeneratedTransactions {
		txHashBase64 := base64.StdEncoding.EncodeToString(genTx.TransactionHash)
		signatureBase64, err := crypto.SignTransactionHash(txHashBase64, privateKeyBase64)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to sign transaction hash: %w", err)
		}

		signatureBytes, err := base64.StdEncoding.DecodeString(signatureBase64)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to decode signature: %w", err)
		}

		onboardingTxs[i] = damlModel.SignedTransaction{
			Transaction: genTx.SerializedTransaction,
			Signatures: []damlModel.Signature{
				{
					Format:               damlModel.SignatureFormatConcat,
					Signature:            signatureBytes,
					SignedBy:             keyFingerprint,
					SigningAlgorithmSpec: damlModel.SigningAlgorithmSpecED25519,
				},
			},
		}
		transactionHashes[i] = genTx.TransactionHash
	}

	multiHash, err := crypto.ComputeMultiHashForTopology(transactionHashes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to compute multi-hash: %w", err)
	}

	multiHashBase64 := base64.StdEncoding.EncodeToString(multiHash)
	multiHashSignatureBase64, err := crypto.SignTransactionHash(multiHashBase64, privateKeyBase64)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to sign multi-hash: %w", err)
	}

	multiHashSignatureBytes, err := base64.StdEncoding.DecodeString(multiHashSignatureBase64)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode multi-hash signature: %w", err)
	}

	multiHashSigs := []damlModel.Signature{
		{
			Format:               damlModel.SignatureFormatConcat,
			Signature:            multiHashSignatureBytes,
			SignedBy:             keyFingerprint,
			SigningAlgorithmSpec: damlModel.SigningAlgorithmSpecED25519,
		},
	}

	return onboardingTxs, multiHashSigs, nil
}
