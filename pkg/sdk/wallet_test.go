package sdk_test

import (
	"context"
	"encoding/base64"
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

	dsoParty, err := cl.PartyMng.AllocateParty(ctx, "dso-"+uuid.New().String()[:8], nil, "")
	require.NoError(t, err)

	_, err = cl.UserMng.GrantUserRights(ctx, "app-provider", "", []*damlModel.Right{
		{Type: damlModel.CanActAs{Party: dsoParty.Party}},
		{Type: damlModel.CanReadAs{Party: dsoParty.Party}},
	})
	require.NoError(t, err)

	t.Logf("DSO Party: %s", dsoParty.Party)
	t.Logf("Synchronizer ID: %s", synchronizerID)

	walletSDK.TokenStandard().SetPartyID(model.PartyID(dsoParty.Party))
	walletSDK.TokenStandard().SetSynchronizerID(synchronizerID)
	walletSDK.Validator().SetPartyID(model.PartyID(dsoParty.Party))
	walletSDK.Validator().SetSynchronizerID(synchronizerID)

	retrievedParty, err := walletSDK.TokenStandard().GetPartyID()
	require.NoError(t, err)
	t.Logf("Retrieved Party ID from controller: %s", retrievedParty)

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

	t.Log("Test setup complete - wallet initialized with balance check")
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
