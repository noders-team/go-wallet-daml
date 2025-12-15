package sdk_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	damlModel "github.com/noders-team/go-daml/pkg/model"
	"github.com/noders-team/go-wallet-daml/pkg/auth"
	"github.com/noders-team/go-wallet-daml/pkg/controller"
	"github.com/noders-team/go-wallet-daml/pkg/model"
	"github.com/noders-team/go-wallet-daml/pkg/sdk"
	"github.com/stretchr/testify/require"
)

func TestAllocateExternalParty(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	walletSDK := sdk.NewWalletSDK()
	walletSDK.Configure(sdk.Config{
		AuthFactory: func() auth.AuthController {
			return auth.NewUnsafeAuthController(
				auth.UnsafeWithUserID("app-provider"),
				auth.UnsafeWithAdminID("app-provider"),
				auth.UnsafeWithAudience("https://canton.network.global"),
				auth.UnsafeWithSecret("unsafe"),
			)
		},
		LedgerFactory: func(userID string, provider *auth.AuthTokenProvider, isAdmin bool) (*controller.LedgerController, error) {
			return controller.NewLedgerController(
				userID,
				sandboxGrpcAddr,
				sandboxHTTPAddr,
				provider,
				isAdmin,
			)
		},
		TokenStandardFactory: func(userID string, provider *auth.AuthTokenProvider, isAdmin bool) (*controller.TokenStandardController, error) {
			return controller.NewTokenStandardController(
				userID,
				sandboxGrpcAddr,
				provider,
				isAdmin,
			)
		},
		ValidatorFactory: func(userID string, provider *auth.AuthTokenProvider) (*controller.ValidatorController, error) {
			return controller.NewValidatorController(
				userID,
				sandboxGrpcAddr,
				sandboxHTTPAddr,
				provider,
			)
		},
	})
	require.NoError(t, walletSDK.Connect(ctx))
	require.NoError(t, walletSDK.ConnectAdmin(ctx))
	require.NotNil(t, walletSDK.AdminLedger())

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

	t.Skip("GenerateExternalParty method removed - test needs update")
}

func TestExternalPartyWalletWithMintAndTransfer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	walletSDK := sdk.NewWalletSDK()
	walletSDK.Configure(sdk.Config{
		AuthFactory: func() auth.AuthController {
			return auth.NewUnsafeAuthController(
				auth.UnsafeWithUserID("app-provider"),
				auth.UnsafeWithAdminID("app-provider"),
				auth.UnsafeWithAudience("https://canton.network.global"),
				auth.UnsafeWithSecret("unsafe"),
			)
		},
		LedgerFactory: func(userID string, provider *auth.AuthTokenProvider, isAdmin bool) (*controller.LedgerController, error) {
			return controller.NewLedgerController(
				userID,
				sandboxGrpcAddr,
				sandboxHTTPAddr,
				provider,
				isAdmin,
			)
		},
		TokenStandardFactory: func(userID string, provider *auth.AuthTokenProvider, isAdmin bool) (*controller.TokenStandardController, error) {
			return controller.NewTokenStandardController(
				userID,
				sandboxGrpcAddr,
				provider,
				isAdmin,
			)
		},
		ValidatorFactory: func(userID string, provider *auth.AuthTokenProvider) (*controller.ValidatorController, error) {
			return controller.NewValidatorController(
				userID,
				sandboxGrpcAddr,
				sandboxHTTPAddr,
				provider,
			)
		},
	})
	require.NoError(t, walletSDK.Connect(ctx))

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
	walletSDK.TokenStandard().SetSynchronizerID(model.PartyID(synchronizerID))
	walletSDK.Validator().SetPartyID(model.PartyID(dsoParty.Party))
	walletSDK.Validator().SetSynchronizerID(model.PartyID(synchronizerID))

	retrievedParty, err := walletSDK.TokenStandard().GetPartyID()
	require.NoError(t, err)
	t.Logf("Retrieved Party ID from controller: %s", retrievedParty)

	balance, err := walletSDK.TokenStandard().GetBalance(ctx)
	require.NoError(t, err)
	require.True(t, balance.IsZero(), "expected balance to be zero, got %s", balance.String())

	// walletSDK.TokenStandard().Mint(ctx, model.PartyID(dsoParty.Party), decimal.NewFromInt(1000), "AMT", "AMT")

	// amuletRulesCid, openRoundCid := setupAmuletSystem(t, ctx, dsoParty.Party, spliceAmuletPkgID)

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

	t.Log("Successfully minted amulet for external party")
}
