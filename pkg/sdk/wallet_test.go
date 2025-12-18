package sdk_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
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
)

func TestAllocateExternalParty(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cl := testutil.GetClient()
	require.NotNil(t, cl)
	require.NotNil(t, cl.PartyMng)
	require.NotNil(t, cl.TopologyManagerWrite)

	syncResp, err := cl.StateService.GetConnectedSynchronizers(ctx, &damlModel.GetConnectedSynchronizersRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, syncResp.ConnectedSynchronizers)

	synchronizerID := syncResp.ConnectedSynchronizers[0].SynchronizerID

	participantID, err := cl.PartyMng.GetParticipantID(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, participantID)

	keyPair, err := crypto.CreateKeyPair()
	require.NoError(t, err)
	require.NotEmpty(t, keyPair.PublicKey)
	require.NotEmpty(t, keyPair.PrivateKey)

	keyFingerprint, err := crypto.CreateFingerprintFromKey(keyPair.PublicKey)
	require.NoError(t, err)
	require.NotEmpty(t, keyFingerprint)

	namespace := keyFingerprint
	partyID := fmt.Sprintf("external-party-%d", time.Now().Unix())
	fullPartyID := fmt.Sprintf("%s::%s", partyID, namespace)

	onboardingTxs, multiHashSigs, err := createValidOnboardingTransactions(
		ctx,
		cl,
		fullPartyID,
		keyPair.PublicKey,
		keyPair.PrivateKey,
		participantID,
		keyFingerprint,
	)
	require.NoError(t, err)
	require.NotEmpty(t, onboardingTxs)

	allocatedPartyID, err := cl.PartyMng.AllocateExternalParty(
		ctx,
		synchronizerID,
		onboardingTxs,
		multiHashSigs,
		"",
	)
	require.NoError(t, err)
	require.NotEmpty(t, allocatedPartyID)

	time.Sleep(2 * time.Second)

	parties, err := cl.PartyMng.ListKnownParties(ctx, "", 100, "")
	require.NoError(t, err)
	require.NotEmpty(t, parties.PartyDetails)

	found := false
	for _, party := range parties.PartyDetails {
		if party.Party == allocatedPartyID {
			found = true
			require.True(t, party.IsLocal, "External party should be marked as local after allocation")
			break
		}
	}
	require.True(t, found, "Allocated external party should appear in ListKnownParties")
}

func TestExternalPartyWalletWithMintAndTransfer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	grpcAddr := testutil.GetGrpcAddr()
	scanProxyURL := testutil.GetScanProxyBaseURL()
	synchronizerID := testutil.GetSynchronizerID()
	dsoPartyID := testutil.GetDsoPartyID()

	walletSDK := sdk.NewWalletSDK()
	walletSDK.Configure(sdk.Config{
		AuthFactory: func() auth.AuthController {
			return auth.NewMockAuthController("app-provider")
		},
		LedgerFactory: func(userID string, provider *auth.AuthTokenProvider, isAdmin bool) (*controller.LedgerController, error) {
			return controller.NewLedgerController(
				userID,
				grpcAddr,
				scanProxyURL,
				provider,
				isAdmin,
			)
		},
		TokenStandardFactory: func(userID string, provider *auth.AuthTokenProvider, isAdmin bool) (*controller.TokenStandardController, error) {
			return controller.NewTokenStandardController(
				userID,
				grpcAddr,
				provider,
				isAdmin,
			)
		},
		ValidatorFactory: func(userID string, provider *auth.AuthTokenProvider) (*controller.ValidatorController, error) {
			return controller.NewValidatorController(
				userID,
				grpcAddr,
				scanProxyURL,
				provider,
			)
		},
	})
	require.NoError(t, walletSDK.Connect(ctx))

	cl := testutil.GetClient()
	packages, err := cl.PackageMng.ListKnownPackages(ctx)
	require.NoError(t, err)

	var spliceAmuletPkgID string
	for _, pkg := range packages {
		if pkg.Name == "splice-amulet" {
			spliceAmuletPkgID = pkg.PackageID
			break
		}
	}
	require.NotEmpty(t, spliceAmuletPkgID, "splice-amulet package not found")

	t.Logf("DSO Party: %s", dsoPartyID)
	t.Logf("Synchronizer ID: %s", synchronizerID)

	walletSDK.TokenStandard().SetPartyID(dsoPartyID)
	walletSDK.TokenStandard().SetSynchronizerID(synchronizerID)
	walletSDK.Validator().SetPartyID(dsoPartyID)
	walletSDK.Validator().SetSynchronizerID(synchronizerID)

	retrievedParty, err := walletSDK.TokenStandard().GetPartyID()
	require.NoError(t, err)
	t.Logf("retrieved Party ID from controller: %s", retrievedParty)

	balance, err := walletSDK.TokenStandard().GetBalance(ctx)
	require.NoError(t, err)
	require.True(t, balance.IsZero(), "expected balance to be zero, got %s", balance.String())

	externalParty, err := cl.PartyMng.AllocateParty(ctx, "external-"+uuid.New().String()[:8], nil, "")
	require.NoError(t, err)

	externalPartyID := model.PartyID(externalParty.Party)

	_, err = cl.UserMng.GrantUserRights(ctx, "app-provider", "", []*damlModel.Right{
		{Type: damlModel.CanActAs{Party: string(externalPartyID)}},
		{Type: damlModel.CanReadAs{Party: string(externalPartyID)}},
	})
	require.NoError(t, err)

	receiverParty, err := cl.PartyMng.AllocateParty(ctx, "receiver-"+uuid.New().String()[:8], nil, "")
	require.NoError(t, err)

	_, err = cl.UserMng.GrantUserRights(ctx, "app-provider", "", []*damlModel.Right{
		{Type: damlModel.CanActAs{Party: receiverParty.Party}},
		{Type: damlModel.CanReadAs{Party: receiverParty.Party}},
	})
	require.NoError(t, err)

	t.Log("Minting amulets to DSO party")
	mintAmount := decimal.NewFromFloat(1000.0)
	tapResult, err := walletSDK.TokenStandard().CreateAndSubmitTapInternal(ctx, dsoPartyID, mintAmount, "", string(dsoPartyID))
	require.NoError(t, err)
	t.Logf("Tap result: %+v", tapResult)

	if updateID, ok := tapResult["updateId"].(string); ok {
		t.Logf("Tap transaction updateId: %s", updateID)

		updatesReq := &damlModel.GetUpdatesRequest{
			BeginExclusive: 0,
			UpdateFormat: &damlModel.EventFormat{
				Verbose: true,
				FiltersByParty: map[string]*damlModel.Filters{
					string(dsoPartyID): {
						Inclusive: &damlModel.InclusiveFilters{},
					},
				},
			},
		}

		updatesStream, updatesErrChan := cl.UpdateService.GetUpdates(ctx, updatesReq)

		timeout := time.After(5 * time.Second)
		foundTapTx := false
		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case resp, ok := <-updatesStream:
					if !ok {
						return
					}
					if resp.Update != nil && resp.Update.Transaction != nil {
						tx := resp.Update.Transaction
						if tx.UpdateID == updateID {
							foundTapTx = true
							t.Logf("Found tap transaction with %d events", len(tx.Events))
							for i, event := range tx.Events {
								if event.Created != nil {
									t.Logf("  Event %d: Created contract TemplateID=%s, ContractID=%s",
										i, event.Created.TemplateID, event.Created.ContractID)
									if args, ok := event.Created.CreateArguments.(map[string]interface{}); ok {
										t.Logf("    Full Arguments: %+v", args)
									}
								}
							}
							return
						}
					}
				case err := <-updatesErrChan:
					if err != nil {
						t.Logf("Error getting updates: %v", err)
						return
					}
				case <-timeout:
					t.Logf("Timeout waiting for tap transaction in updates stream")
					return
				}
			}
		}()

		if !foundTapTx {
			t.Logf("warning: Could not find tap transaction in updates stream")
		}
	}

	t.Log("verifying DSO party balance after mint")

	if completionOffset, ok := tapResult["completionOffset"].(int64); ok {
		t.Logf("tap completed at offset: %d", completionOffset)
	}

	t.Log("Waiting 10 seconds for indexer to process the transaction...")
	time.Sleep(10 * time.Second)

	t.Log("Checking if Amulet contract is visible to DSO...")
	amuletTemplateID := fmt.Sprintf("%s:Splice.Amulet:Amulet", spliceAmuletPkgID)
	t.Logf("Template ID: %s", amuletTemplateID)
	t.Logf("DSO Party: %s", dsoPartyID)

	err = walletSDK.SetPartyID(ctx, dsoPartyID, &synchronizerID)
	require.NoError(t, err)

	dsoHoldings, err := walletSDK.TokenStandard().ListHoldingUtxos(ctx, true, 100)
	require.NoError(t, err)
	require.Greater(t, len(dsoHoldings), 0, "DSO should have at least one holding after minting")

	dsoBalance := decimal.Zero
	for _, holding := range dsoHoldings {
		dsoBalance = dsoBalance.Add(holding.Amount)
	}
	t.Logf("DSO balance after mint: %s (from %d holdings)", dsoBalance.String(), len(dsoHoldings))
	require.True(t, dsoBalance.GreaterThan(decimal.Zero), "DSO balance should be greater than zero after minting, got %s", dsoBalance.String())

	walletSDK.TokenStandard().SetPartyID(model.PartyID(externalParty.Party))
	externalBalance, err := walletSDK.TokenStandard().GetBalance(ctx)
	require.NoError(t, err)
	require.True(t, externalBalance.IsZero(), "external party balance should be zero, got %s", externalBalance.String())

	walletSDK.TokenStandard().SetPartyID(model.PartyID(receiverParty.Party))
	receiverBalance, err := walletSDK.TokenStandard().GetBalance(ctx)
	require.NoError(t, err)
	require.True(t, receiverBalance.IsZero(), "receiver party balance should be zero, got %s", receiverBalance.String())

	walletSDK.TokenStandard().SetPartyID(dsoPartyID)

	t.Log("Creating transfer from DSO to external party")
	transferAmount := decimal.NewFromFloat(500.0)

	holdings, err := walletSDK.TokenStandard().ListHoldingUtxos(ctx, false, 100)
	require.NoError(t, err)
	require.NotEmpty(t, holdings, "DSO party should have holding UTXOs")

	var inputUtxos []string
	for _, holding := range holdings {
		inputUtxos = append(inputUtxos, holding.ContractID)
		if holding.Amount.GreaterThanOrEqual(transferAmount) {
			break
		}
	}
	require.NotEmpty(t, inputUtxos, "should have input UTXOs for transfer")

	transferResult, err := walletSDK.TokenStandard().CreateTransfer(
		ctx,
		dsoPartyID,
		externalPartyID,
		transferAmount,
		holdings[0].InstrumentID,
		holdings[0].InstrumentAdmin,
		inputUtxos,
		"test-transfer-to-external",
	)
	require.NoError(t, err)
	require.NotNil(t, transferResult)

	submitReq := &damlModel.SubmitAndWaitRequest{
		Commands: &damlModel.Commands{
			UserID:    "app-provider",
			CommandID: fmt.Sprintf("transfer-%d", time.Now().UnixNano()),
			Commands:  []*damlModel.Command{transferResult.Command},
			ActAs:     []string{string(dsoPartyID), string(externalPartyID)},
			ReadAs:    []string{},
		},
	}

	transferResp, err := cl.CommandService.SubmitAndWait(ctx, submitReq)
	require.NoError(t, err)
	t.Logf("Transfer submitted successfully, updateID: %s", transferResp.UpdateID)

	updatesReq := &damlModel.GetUpdatesRequest{
		BeginExclusive: 0,
		UpdateFormat: &damlModel.EventFormat{
			Verbose: true,
			FiltersByParty: map[string]*damlModel.Filters{
				string(dsoPartyID): {
					Inclusive: &damlModel.InclusiveFilters{},
				},
				string(externalPartyID): {
					Inclusive: &damlModel.InclusiveFilters{},
				},
			},
		},
	}

	updatesStream, updatesErrChan := cl.UpdateService.GetUpdates(ctx, updatesReq)
	timeout := time.After(5 * time.Second)
	foundTransferTx := false
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case resp, ok := <-updatesStream:
				if !ok {
					return
				}
				if resp.Update != nil && resp.Update.Transaction != nil {
					tx := resp.Update.Transaction
					if tx.UpdateID == transferResp.UpdateID {
						foundTransferTx = true
						t.Logf("Found transfer transaction with %d events", len(tx.Events))
						for i, event := range tx.Events {
							if event.Created != nil {
								t.Logf("  Event %d: Created contract TemplateID=%s, ContractID=%s",
									i, event.Created.TemplateID, event.Created.ContractID)
								if args, ok := event.Created.CreateArguments.(map[string]interface{}); ok {
									if owner, ok := args["owner"]; ok {
										t.Logf("    Owner: %+v", owner)
									}
									if amount, ok := args["amount"]; ok {
										t.Logf("    Amount: %+v", amount)
									}
								}
								t.Logf("    Signatories: %+v", event.Created.Signatories)
								t.Logf("    Observers: %+v", event.Created.Observers)
							} else if event.Archived != nil {
								t.Logf("  Event %d: Archived contract TemplateID=%s, ContractID=%s",
									i, event.Archived.TemplateID, event.Archived.ContractID)
							}
						}
						return
					}
				}
			case err := <-updatesErrChan:
				if err != nil {
					t.Logf("Error getting updates: %v", err)
					return
				}
			case <-timeout:
				t.Logf("Timeout waiting for transfer transaction in updates stream")
				return
			}
		}
	}()

	if !foundTransferTx {
		t.Logf("Warning: Could not find transfer transaction in updates stream")
	}

	time.Sleep(3 * time.Second)

	t.Log("Verifying balances after first transfer")
	walletSDK.TokenStandard().SetPartyID(model.PartyID(externalParty.Party))

	externalHoldingsAfterTransfer, err := walletSDK.TokenStandard().ListHoldingUtxos(ctx, true, 100)
	require.NoError(t, err)

	externalBalanceAfterTransfer := decimal.Zero
	for _, holding := range externalHoldingsAfterTransfer {
		externalBalanceAfterTransfer = externalBalanceAfterTransfer.Add(holding.Amount)
	}
	t.Logf("external party balance after transfer: %s (from %d holdings)", externalBalanceAfterTransfer.String(), len(externalHoldingsAfterTransfer))
	require.True(t, externalBalanceAfterTransfer.GreaterThan(decimal.Zero), "external party balance should be greater than zero after transfer, got %s", externalBalanceAfterTransfer.String())

	t.Log("creating transfer from external party to receiver")
	finalTransferAmount := decimal.NewFromFloat(200.0)

	externalHoldings, err := walletSDK.TokenStandard().ListHoldingUtxos(ctx, false, 100)
	require.NoError(t, err)
	require.NotEmpty(t, externalHoldings, "external party should have holding UTXOs")

	var externalInputUtxos []string
	for _, holding := range externalHoldings {
		externalInputUtxos = append(externalInputUtxos, holding.ContractID)
		if holding.Amount.GreaterThanOrEqual(finalTransferAmount) {
			break
		}
	}
	require.NotEmpty(t, externalInputUtxos, "should have input UTXOs for final transfer")

	finalTransferResult, err := walletSDK.TokenStandard().CreateTransfer(
		ctx,
		externalPartyID,
		model.PartyID(receiverParty.Party),
		finalTransferAmount,
		externalHoldings[0].InstrumentID,
		string(dsoPartyID),
		externalInputUtxos,
		"test-transfer-to-receiver",
	)
	require.NoError(t, err)
	require.NotNil(t, finalTransferResult)

	finalSubmitReq := &damlModel.SubmitAndWaitRequest{
		Commands: &damlModel.Commands{
			UserID:    "app-provider",
			CommandID: fmt.Sprintf("transfer-%d", time.Now().UnixNano()),
			Commands:  []*damlModel.Command{finalTransferResult.Command},
			ActAs:     []string{string(dsoPartyID), externalParty.Party, receiverParty.Party},
			ReadAs:    []string{},
		},
	}

	finalTransferResp, err := cl.CommandService.SubmitAndWait(ctx, finalSubmitReq)
	require.NoError(t, err)
	t.Logf("final transfer submitted successfully, updateID: %s", finalTransferResp.UpdateID)

	time.Sleep(3 * time.Second)

	t.Log("verifying final balances")
	walletSDK.TokenStandard().SetPartyID(model.PartyID(receiverParty.Party))

	receiverFinalHoldings, err := walletSDK.TokenStandard().ListHoldingUtxos(ctx, true, 100)
	require.NoError(t, err)

	receiverFinalBalance := decimal.Zero
	for _, holding := range receiverFinalHoldings {
		receiverFinalBalance = receiverFinalBalance.Add(holding.Amount)
	}
	t.Logf("Receiver party final balance: %s (from %d holdings)", receiverFinalBalance.String(), len(receiverFinalHoldings))
	require.True(t, receiverFinalBalance.GreaterThan(decimal.Zero), "receiver party balance should be greater than zero after final transfer, got %s", receiverFinalBalance.String())

	walletSDK.TokenStandard().SetPartyID(model.PartyID(externalParty.Party))
	externalFinalHoldings, err := walletSDK.TokenStandard().ListHoldingUtxos(ctx, true, 100)
	require.NoError(t, err)

	externalFinalBalance := decimal.Zero
	for _, holding := range externalFinalHoldings {
		externalFinalBalance = externalFinalBalance.Add(holding.Amount)
	}
	t.Logf("External party final balance: %s (from %d holdings)", externalFinalBalance.String(), len(externalFinalHoldings))

	walletSDK.TokenStandard().SetPartyID(dsoPartyID)
	dsoFinalHoldings, err := walletSDK.TokenStandard().ListHoldingUtxos(ctx, true, 100)
	require.NoError(t, err)

	dsoFinalBalance := decimal.Zero
	for _, holding := range dsoFinalHoldings {
		dsoFinalBalance = dsoFinalBalance.Add(holding.Amount)
	}
	t.Logf("DSO party final balance: %s (from %d holdings)", dsoFinalBalance.String(), len(dsoFinalHoldings))

	t.Log("Test completed successfully - mint and transfer operations verified")
}

func createValidOnboardingTransactions(
	ctx context.Context,
	adminCl *client.DamlBindingClient,
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

	genResp, err := adminCl.TopologyManagerWrite.GenerateTransactions(ctx, &damlModel.GenerateTransactionsRequest{
		Proposals: proposals,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate transactions: %w", err)
	}

	if len(genResp.GeneratedTransactions) != len(proposals) {
		return nil, nil, fmt.Errorf("expected %d transactions, got %d", len(proposals), len(genResp.GeneratedTransactions))
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
