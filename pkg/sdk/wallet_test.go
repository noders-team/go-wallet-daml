package sdk_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	damlModel "github.com/noders-team/go-daml/pkg/model"
	"github.com/noders-team/go-wallet-daml/pkg/auth"
	"github.com/noders-team/go-wallet-daml/pkg/controller"
	"github.com/noders-team/go-wallet-daml/pkg/model"
	"github.com/noders-team/go-wallet-daml/pkg/sdk"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func toNumeric(f float64) *big.Int {
	multiplier := new(big.Int).Exp(big.NewInt(10), big.NewInt(10), nil)
	floatVal := big.NewFloat(f)
	floatVal.Mul(floatVal, new(big.Float).SetInt(multiplier))
	result, _ := floatVal.Int(nil)
	return result
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
				"http://"+sandboxGrpcAddr,
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
				"http://"+sandboxGrpcAddr,
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

	walletSDK.TokenStandard().SetPartyID(model.PartyID(dsoParty.Party))
	walletSDK.TokenStandard().SetSynchronizerID(model.PartyID(synchronizerID))
	walletSDK.Validator().SetPartyID(model.PartyID(dsoParty.Party))
	walletSDK.Validator().SetSynchronizerID(model.PartyID(synchronizerID))

	balance, err := walletSDK.TokenStandard().GetBalance(ctx)
	require.NoError(t, err)
	require.Zero(t, balance)

	amuletRulesCid, openRoundCid := setupAmuletSystem(t, ctx, dsoParty.Party, spliceAmuletPkgID)

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

	mintAmount := decimal.NewFromFloat(100.0)
	amuletCid := tapAmulet(t, ctx, dsoParty.Party, string(externalPartyID), mintAmount, amuletRulesCid, openRoundCid, spliceAmuletPkgID)

	time.Sleep(2 * time.Second)
	require.NotEmpty(t, amuletCid)

	t.Log("Successfully minted amulet for external party")
}

func setupAmuletSystem(t *testing.T, ctx context.Context, dsoParty, pkgID string) (string, string) {
	defaultAmuletConfig := map[string]interface{}{
		"transferConfig": map[string]interface{}{
			"createFee":                    map[string]interface{}{"fee": toNumeric(0.03)},
			"holdingFee":                   map[string]interface{}{"rate": toNumeric(0.0000048225)},
			"lockHolderFee":                map[string]interface{}{"fee": toNumeric(0.005)},
			"transferFee":                  map[string]interface{}{"initialRate": toNumeric(0.01), "steps": []interface{}{}},
			"extraFeaturedAppRewardAmount": toNumeric(1.0),
			"maxNumInputs":                 100,
			"maxNumOutputs":                100,
			"maxNumLockHolders":            50,
		},
		"issuanceCurve": map[string]interface{}{
			"initialValue": map[string]interface{}{
				"amuletToIssuePerYear":      toNumeric(40000000000.0),
				"validatorRewardPercentage": toNumeric(0.05),
				"appRewardPercentage":       toNumeric(0.15),
				"validatorRewardCap":        toNumeric(0.2),
				"featuredAppRewardCap":      toNumeric(100.0),
				"unfeaturedAppRewardCap":    toNumeric(0.6),
				"optValidatorFaucetCap": map[string]interface{}{
					"_type": "optional",
				},
			},
			"futureValues": []interface{}{},
		},
		"decentralizedSynchronizer": map[string]interface{}{
			"requiredSynchronizers": []string{synchronizerID},
			"activeSynchronizer":    synchronizerID,
			"fees":                  map[string]interface{}{"baseRateTrafficLimits": map[string]interface{}{"burstAmount": 400000, "burstWindow": map[string]interface{}{"microseconds": 1200000000}}, "extraTrafficPrice": toNumeric(16.67), "readVsWriteScalingFactor": 4, "minTopupAmount": 200000},
		},
		"tickDuration": map[string]interface{}{"microseconds": 600000000},
		"packageConfig": map[string]interface{}{
			"amulet":             "0.1.4",
			"amuletNameService":  "0.1.2",
			"dsoGovernance":      "0.1.4",
			"validatorLifecycle": "0.1.1",
			"wallet":             "0.1.3",
			"walletPayments":     "0.1.2",
		},
	}

	configSchedule := map[string]interface{}{
		"initialValue": defaultAmuletConfig,
		"futureValues": []interface{}{},
	}

	createAmuletRulesCmd := damlModel.Command{
		Command: &damlModel.CreateCommand{
			TemplateID: pkgID + ":Splice.AmuletRules:AmuletRules",
			Arguments: map[string]interface{}{
				"dso": map[string]interface{}{
					"_type": "party",
					"value": dsoParty,
				},
				"configSchedule": configSchedule,
				"isDevNet":       true,
			},
		},
	}

	cmdID := uuid.New().String()
	createReq := &damlModel.SubmitRequest{
		Commands: &damlModel.Commands{
			UserID:    "app-provider",
			CommandID: cmdID,
			Commands:  []*damlModel.Command{&createAmuletRulesCmd},
			ActAs:     []string{dsoParty},
			ReadAs:    []string{},
		},
	}

	_, err := cl.CommandSubmission.Submit(ctx, createReq)
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	filter := &damlModel.TransactionFilter{
		FiltersByParty: map[string]*damlModel.Filters{
			dsoParty: {
				Inclusive: &damlModel.InclusiveFilters{
					TemplateFilters: []*damlModel.TemplateFilter{
						{TemplateID: pkgID + ":Splice.AmuletRules:AmuletRules"},
					},
				},
			},
		},
	}

	stream, errChan := cl.StateService.GetActiveContracts(ctx, &damlModel.GetActiveContractsRequest{Filter: filter})

	var amuletRulesCid string
	select {
	case resp := <-stream:
		for _, contract := range resp.ActiveContracts {
			amuletRulesCid = contract.ContractID
			break
		}
	case err := <-errChan:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		require.Fail(t, "timeout waiting for AmuletRules")
	}
	require.NotEmpty(t, amuletRulesCid)

	createExtRulesCmd := damlModel.Command{
		Command: &damlModel.CreateCommand{
			TemplateID: pkgID + ":Splice.ExternalPartyAmuletRules:ExternalPartyAmuletRules",
			Arguments: map[string]interface{}{
				"dso": map[string]interface{}{
					"_type": "party",
					"value": dsoParty,
				},
			},
		},
	}

	cmdID2 := uuid.New().String()
	createExtReq := &damlModel.SubmitRequest{
		Commands: &damlModel.Commands{
			UserID:    "app-provider",
			CommandID: cmdID2,
			Commands:  []*damlModel.Command{&createExtRulesCmd},
			ActAs:     []string{dsoParty},
			ReadAs:    []string{},
		},
	}

	_, err = cl.CommandSubmission.Submit(ctx, createExtReq)
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	bootstrapCmd := damlModel.Command{
		Command: &damlModel.ExerciseCommand{
			ContractID: amuletRulesCid,
			TemplateID: pkgID + ":Splice.AmuletRules:AmuletRules",
			Choice:     "AmuletRules_Bootstrap_Rounds",
			Arguments: map[string]interface{}{
				"amuletPrice":    toNumeric(0.5),
				"round0Duration": map[string]interface{}{"microseconds": 600000000},
				"initialRound": map[string]interface{}{
					"_type": "optional",
				},
			},
		},
	}

	cmdID3 := uuid.New().String()
	bootstrapReq := &damlModel.SubmitRequest{
		Commands: &damlModel.Commands{
			UserID:    "app-provider",
			CommandID: cmdID3,
			Commands:  []*damlModel.Command{&bootstrapCmd},
			ActAs:     []string{dsoParty},
			ReadAs:    []string{},
		},
	}

	_, err = cl.CommandSubmission.Submit(ctx, bootstrapReq)
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	roundFilter := &damlModel.TransactionFilter{
		FiltersByParty: map[string]*damlModel.Filters{
			dsoParty: {
				Inclusive: &damlModel.InclusiveFilters{
					TemplateFilters: []*damlModel.TemplateFilter{
						{TemplateID: pkgID + ":Splice.Round:OpenMiningRound"},
					},
				},
			},
		},
	}

	roundStream, roundErrChan := cl.StateService.GetActiveContracts(ctx, &damlModel.GetActiveContractsRequest{Filter: roundFilter})

	var openRoundCid string
	select {
	case resp := <-roundStream:
		for _, contract := range resp.ActiveContracts {
			openRoundCid = contract.ContractID
			break
		}
	case err := <-roundErrChan:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		require.Fail(t, "timeout waiting for OpenMiningRound")
	}
	require.NotEmpty(t, openRoundCid)

	return amuletRulesCid, openRoundCid
}

func tapAmulet(t *testing.T, ctx context.Context, dsoParty, receiver string, amount decimal.Decimal, amuletRulesCid, openRoundCid, pkgID string) string {
	tapCmd := damlModel.Command{
		Command: &damlModel.ExerciseCommand{
			ContractID: amuletRulesCid,
			TemplateID: pkgID + ":Splice.AmuletRules:AmuletRules",
			Choice:     "AmuletRules_DevNet_Tap",
			Arguments: map[string]interface{}{
				"receiver": map[string]interface{}{
					"_type": "party",
					"value": receiver,
				},
				"amount":    amount.String(),
				"openRound": openRoundCid,
			},
		},
	}

	cmdID := uuid.New().String()
	tapReq := &damlModel.SubmitRequest{
		Commands: &damlModel.Commands{
			UserID:    "app-provider",
			CommandID: cmdID,
			Commands:  []*damlModel.Command{&tapCmd},
			ActAs:     []string{dsoParty, receiver},
			ReadAs:    []string{},
		},
	}

	_, err := cl.CommandSubmission.Submit(ctx, tapReq)
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	filter := &damlModel.TransactionFilter{
		FiltersByParty: map[string]*damlModel.Filters{
			receiver: {
				Inclusive: &damlModel.InclusiveFilters{
					TemplateFilters: []*damlModel.TemplateFilter{
						{TemplateID: pkgID + ":Splice.Amulet:Amulet"},
					},
				},
			},
		},
	}

	stream, errChan := cl.StateService.GetActiveContracts(ctx, &damlModel.GetActiveContractsRequest{Filter: filter})

	var amuletCid string
	select {
	case resp := <-stream:
		for _, contract := range resp.ActiveContracts {
			amuletCid = contract.ContractID
			break
		}
	case err := <-errChan:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		require.Fail(t, "timeout waiting for Amulet")
	}

	return amuletCid
}
