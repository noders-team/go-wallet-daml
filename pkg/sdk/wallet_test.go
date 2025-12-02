package sdk_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	damlModel "github.com/noders-team/go-daml/pkg/model"
	"github.com/noders-team/go-wallet-daml/pkg/crypto"
	"github.com/noders-team/go-wallet-daml/pkg/model"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestExternalPartyWalletWithMintAndTransfer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	_, err := crypto.CreateKeyPair()
	require.NoError(t, err)

	_, err = cl.PartyMng.GetParticipantID(ctx)
	require.NoError(t, err)

	providerParty, err := cl.PartyMng.AllocateParty(ctx, "provider-"+uuid.New().String()[:8], nil, "")
	require.NoError(t, err)

	syncResp, err := cl.StateService.GetConnectedSynchronizers(ctx, &damlModel.GetConnectedSynchronizersRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, syncResp.ConnectedSynchronizers)

	externalParty, err := cl.PartyMng.AllocateParty(ctx, "external-"+uuid.New().String()[:8], nil, "")
	require.NoError(t, err)

	externalPartyID := model.PartyID(externalParty.Party)

	_, err = cl.UserMng.GrantUserRights(ctx, "app-provider", "", []*damlModel.Right{
		{Type: damlModel.CanActAs{Party: string(externalPartyID)}},
		{Type: damlModel.CanReadAs{Party: string(externalPartyID)}},
		{Type: damlModel.CanActAs{Party: providerParty.Party}},
		{Type: damlModel.CanReadAs{Party: providerParty.Party}},
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

	createAmuletCmd := damlModel.Command{
		Command: damlModel.CreateCommand{
			TemplateID: "#splice-amulet:Splice.Amulet:Amulet",
			Arguments: map[string]interface{}{
				"owner":  string(externalPartyID),
				"amount": map[string]interface{}{"_1": mintAmount.String()},
			},
		},
	}

	createCmdID := uuid.New().String()
	createReq := &damlModel.SubmitRequest{
		Commands: &damlModel.Commands{
			UserID:    "app-provider",
			CommandID: createCmdID,
			Commands:  []*damlModel.Command{&createAmuletCmd},
			ActAs:     []string{providerParty.Party},
			ReadAs:    []string{},
		},
	}

	_, err = cl.CommandSubmission.Submit(ctx, createReq)
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	filter := &damlModel.TransactionFilter{
		FiltersByParty: map[string]*damlModel.Filters{
			string(externalPartyID): {
				Inclusive: &damlModel.InclusiveFilters{
					TemplateFilters: []*damlModel.TemplateFilter{
						{
							TemplateID:              "#splice-amulet:Splice.Amulet:Amulet",
							IncludeCreatedEventBlob: false,
						},
					},
				},
			},
		},
	}

	acsReq := &damlModel.GetActiveContractsRequest{
		Filter: filter,
	}

	stream, errChan := cl.StateService.GetActiveContracts(ctx, acsReq)

	var amuletContractID string
	foundAmulet := false

	for !foundAmulet {
		select {
		case resp, ok := <-stream:
			if !ok {
				break
			}
			for _, contract := range resp.ActiveContracts {
				if contract.TemplateID == "#splice-amulet:Splice.Amulet:Amulet" {
					amuletContractID = contract.ContractID
					foundAmulet = true
					break
				}
			}
		case err := <-errChan:
			if err != nil {
				require.NoError(t, err)
			}
		case <-time.After(5 * time.Second):
			require.Fail(t, "timeout waiting for amulet contract")
		}
	}

	require.NotEmpty(t, amuletContractID)

	transferAmount := decimal.NewFromFloat(10.0)

	transferCmd := damlModel.Command{
		Command: damlModel.ExerciseCommand{
			ContractID: amuletContractID,
			TemplateID: "#splice-amulet:Splice.Amulet:Amulet",
			Choice:     "Transfer",
			Arguments: map[string]interface{}{
				"newOwner": receiverParty.Party,
				"amount":   map[string]interface{}{"_1": transferAmount.String()},
			},
		},
	}

	transferCmdID := uuid.New().String()
	transferReq := &damlModel.SubmitRequest{
		Commands: &damlModel.Commands{
			UserID:    "app-provider",
			CommandID: transferCmdID,
			Commands:  []*damlModel.Command{&transferCmd},
			ActAs:     []string{string(externalPartyID)},
			ReadAs:    []string{},
		},
	}

	_, err = cl.CommandSubmission.Submit(ctx, transferReq)
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	receiverFilter := &damlModel.TransactionFilter{
		FiltersByParty: map[string]*damlModel.Filters{
			receiverParty.Party: {
				Inclusive: &damlModel.InclusiveFilters{
					TemplateFilters: []*damlModel.TemplateFilter{
						{
							TemplateID:              "#splice-amulet:Splice.Amulet:Amulet",
							IncludeCreatedEventBlob: false,
						},
					},
				},
			},
		},
	}

	receiverAcsReq := &damlModel.GetActiveContractsRequest{
		Filter: receiverFilter,
	}

	receiverStream, receiverErrChan := cl.StateService.GetActiveContracts(ctx, receiverAcsReq)

	foundReceiverAmulet := false
	for !foundReceiverAmulet {
		select {
		case resp, ok := <-receiverStream:
			if !ok {
				break
			}
			for _, contract := range resp.ActiveContracts {
				if args, ok := contract.CreateArguments.(map[string]interface{}); ok {
					if owner, ok := args["owner"].(string); ok {
						if owner == receiverParty.Party {
							foundReceiverAmulet = true
							break
						}
					}
				}
			}
		case err := <-receiverErrChan:
			if err != nil {
				require.NoError(t, err)
			}
		case <-time.After(5 * time.Second):
			require.Fail(t, "timeout waiting for receiver amulet contract")
		}
	}

	require.True(t, foundReceiverAmulet)
}

