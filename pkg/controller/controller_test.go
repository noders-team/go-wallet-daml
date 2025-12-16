package controller_test

import (
	"context"
	"os"
	"testing"

	"github.com/noders-team/go-wallet-daml/pkg/testutil"
	"github.com/rs/zerolog/log"
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	if err := testutil.Setup(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to setup test environment")
	}

	code := m.Run()

	testutil.Teardown()

	os.Exit(code)
}

func TestLedgerController(t *testing.T) {
	ctx := context.Background()
	ctrl := testutil.GetLedgerController()

	if ctrl == nil {
		t.Fatal("ledger controller is nil")
	}

	partyID, err := ctrl.GetPartyID()
	if err != nil {
		t.Fatalf("failed to get party ID: %v", err)
	}

	t.Logf("Party ID: %s", partyID)

	syncID, err := ctrl.GetSynchronizerID()
	if err != nil {
		t.Fatalf("failed to get synchronizer ID: %v", err)
	}

	t.Logf("Synchronizer ID: %s", syncID)

	ledgerEnd, err := ctrl.LedgerEnd(ctx)
	if err != nil {
		t.Fatalf("failed to get ledger end: %v", err)
	}

	t.Logf("Ledger end: %d", ledgerEnd)
}

func TestTokenStandardController(t *testing.T) {
	ctx := context.Background()
	ctrl := testutil.GetTokenStandardController()

	if ctrl == nil {
		t.Fatal("token standard controller is nil")
	}

	partyID, err := ctrl.GetPartyID()
	if err != nil {
		t.Fatalf("failed to get party ID: %v", err)
	}

	t.Logf("Party ID: %s", partyID)

	balance, err := ctrl.GetBalance(ctx)
	if err != nil {
		t.Fatalf("failed to get balance: %v", err)
	}

	t.Logf("Balance: %s", balance.String())
}

func TestValidatorController(t *testing.T) {
	ctrl := testutil.GetValidatorController()

	if ctrl == nil {
		t.Fatal("validator controller is nil")
	}

	partyID, err := ctrl.GetPartyID()
	if err != nil {
		t.Fatalf("failed to get party ID: %v", err)
	}

	t.Logf("Party ID: %s", partyID)
}
