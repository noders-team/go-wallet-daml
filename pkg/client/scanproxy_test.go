package client

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/noders-team/go-wallet-daml/pkg/auth"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func loadEnv(t *testing.T) {
	t.Helper()

	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	envPath := filepath.Join(root, ".env")

	f, err := os.Open(envPath)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}

func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	val := os.Getenv(key)
	if val == "" {
		t.Skipf("env %s not set, skipping", key)
	}
	return val
}

func newTestScanProxy(t *testing.T) *ScanProxyClient {
	t.Helper()
	loadEnv(t)

	scanURL := envOrSkip(t, "SCAN_URL")
	oidcURL := envOrSkip(t, "OIDC_URL")
	clientID := envOrSkip(t, "OIDC_CLIENT_ID")
	clientSec := envOrSkip(t, "OIDC_CLIENT_SECRET")
	audience := envOrSkip(t, "OIDC_AUDIENCE")

	authCtrl := auth.NewClientCredentialOAuthController(
		oidcURL,
		auth.WithUserID(clientID),
		auth.WithUserSecret(clientSec),
		auth.WithAudience(audience),
		auth.WithLogger(zerolog.Nop()),
	)
	provider := auth.NewAuthTokenProvider(authCtrl)

	return NewScanProxyClient(scanURL, provider, false)
}

func TestGetOpenMiningRounds_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sp := newTestScanProxy(t)

	rounds, err := sp.GetOpenMiningRounds(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, rounds)

	for i, round := range rounds {
		require.NotEmpty(t, round.ContractID, "round[%d] contract_id empty", i)
		require.NotEmpty(t, round.TemplateID, "round[%d] template_id empty", i)
		require.NotNil(t, round.Payload, "round[%d] payload nil", i)
		require.Contains(t, round.TemplateID, "OpenMiningRound", "round[%d] template_id should reference OpenMiningRound", i)
	}
}

func TestGetOpenMiningRounds_PayloadFields(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sp := newTestScanProxy(t)

	rounds, err := sp.GetOpenMiningRounds(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, rounds)

	round := rounds[0]

	opensAtRaw, ok := round.Payload["opensAt"]
	require.True(t, ok, "payload missing 'opensAt'")
	opensAtStr, ok := opensAtRaw.(string)
	require.True(t, ok, "opensAt should be string, got %T", opensAtRaw)
	opensAt, err := time.Parse(time.RFC3339, opensAtStr)
	require.NoError(t, err, "opensAt should parse as RFC3339")

	closesRaw, ok := round.Payload["targetClosesAt"]
	require.True(t, ok, "payload missing 'targetClosesAt'")
	closesStr, ok := closesRaw.(string)
	require.True(t, ok, "targetClosesAt should be string, got %T", closesRaw)
	closesAt, err := time.Parse(time.RFC3339, closesStr)
	require.NoError(t, err, "targetClosesAt should parse as RFC3339")

	require.True(t, closesAt.After(opensAt), "targetClosesAt should be after opensAt")

	roundField, ok := round.Payload["round"]
	require.True(t, ok, "payload missing 'round'")
	roundMap, ok := roundField.(map[string]any)
	require.True(t, ok, "round field should be a map, got %T", roundField)
	_, ok = roundMap["number"]
	require.True(t, ok, "round should contain 'number'")
}

func TestGetIssuingMiningRounds_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sp := newTestScanProxy(t)

	rounds, err := sp.GetIssuingMiningRounds(ctx)
	require.NoError(t, err)
	require.NotNil(t, rounds)

	for i, round := range rounds {
		require.NotEmpty(t, round.ContractID, "round[%d] contract_id empty", i)
		require.NotEmpty(t, round.TemplateID, "round[%d] template_id empty", i)
		require.Contains(t, round.TemplateID, "IssuingMiningRound", "round[%d] template_id should reference IssuingMiningRound", i)
	}
}

func TestGetIssuingMiningRounds_PayloadFields(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sp := newTestScanProxy(t)

	rounds, err := sp.GetIssuingMiningRounds(ctx)
	require.NoError(t, err)

	if len(rounds) == 0 {
		t.Skip("no issuing rounds on devnet, skipping")
	}

	round := rounds[0]
	require.NotNil(t, round.Payload)

	roundField, ok := round.Payload["round"]
	require.True(t, ok, "payload missing 'round'")
	roundMap, ok := roundField.(map[string]any)
	require.True(t, ok, "round field should be a map, got %T", roundField)
	_, ok = roundMap["number"]
	require.True(t, ok, "round should contain 'number'")

	_, ok = round.Payload["opensAt"]
	require.True(t, ok, "payload missing 'opensAt'")

	_, ok = round.Payload["targetClosesAt"]
	require.True(t, ok, "payload missing 'targetClosesAt'")
}

func TestGetIssuingMiningRounds_DistinctFromOpen(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sp := newTestScanProxy(t)

	open, err := sp.GetOpenMiningRounds(ctx)
	require.NoError(t, err)

	issuing, err := sp.GetIssuingMiningRounds(ctx)
	require.NoError(t, err)

	openIDs := make(map[string]bool, len(open))
	for _, r := range open {
		openIDs[r.ContractID] = true
	}

	for i, r := range issuing {
		require.False(t, openIDs[r.ContractID], "issuing round[%d] should not overlap with open rounds", i)
	}
}

func TestGetClosedMiningRounds_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sp := newTestScanProxy(t)

	closed, err := sp.GetClosedMiningRounds(ctx)
	require.NoError(t, err)
	require.NotNil(t, closed)

	now := time.Now()
	for i, round := range closed {
		closesStr, ok := round.Payload["targetClosesAt"].(string)
		require.True(t, ok, "round[%d] missing targetClosesAt", i)
		closesAt, err := time.Parse(time.RFC3339, closesStr)
		require.NoError(t, err)
		require.True(t, now.After(closesAt), "round[%d] targetClosesAt should be in the past", i)
	}
}

func TestGetClosedMiningRounds_SubsetOfOpen(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sp := newTestScanProxy(t)

	open, err := sp.GetOpenMiningRounds(ctx)
	require.NoError(t, err)

	closed, err := sp.GetClosedMiningRounds(ctx)
	require.NoError(t, err)

	openIDs := make(map[string]bool, len(open))
	for _, r := range open {
		openIDs[r.ContractID] = true
	}

	for i, r := range closed {
		require.True(t, openIDs[r.ContractID], "closed round[%d] should be a subset of open rounds", i)
	}
}

func TestGetClosedMiningRounds_AllHaveValidTimestamps(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sp := newTestScanProxy(t)

	closed, err := sp.GetClosedMiningRounds(ctx)
	require.NoError(t, err)

	for i, round := range closed {
		opensStr, ok := round.Payload["opensAt"].(string)
		require.True(t, ok, "round[%d] missing opensAt", i)
		opensAt, err := time.Parse(time.RFC3339, opensStr)
		require.NoError(t, err, "round[%d] opensAt parse error", i)

		closesStr, ok := round.Payload["targetClosesAt"].(string)
		require.True(t, ok, "round[%d] missing targetClosesAt", i)
		closesAt, err := time.Parse(time.RFC3339, closesStr)
		require.NoError(t, err, "round[%d] targetClosesAt parse error", i)

		require.True(t, closesAt.After(opensAt), "round[%d] targetClosesAt should be after opensAt", i)
	}
}

func TestGetAmuletRules_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sp := newTestScanProxy(t)

	rules, err := sp.GetAmuletRules(ctx)
	require.NoError(t, err)
	require.NotNil(t, rules)
	require.NotEmpty(t, rules.ContractID)
	require.NotEmpty(t, rules.TemplateID)
	require.NotNil(t, rules.Payload)
	require.Contains(t, rules.TemplateID, "AmuletRules")
}

func TestGetAmuletRules_PayloadFields(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sp := newTestScanProxy(t)

	rules, err := sp.GetAmuletRules(ctx)
	require.NoError(t, err)
	require.NotNil(t, rules)

	dso, ok := rules.Payload["dso"].(string)
	require.True(t, ok, "payload missing 'dso' string")
	require.NotEmpty(t, dso)
	require.Contains(t, dso, "DSO::")

	_, ok = rules.Payload["configSchedule"]
	require.True(t, ok, "payload missing 'configSchedule'")
}

func TestGetDSOPartyID_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sp := newTestScanProxy(t)

	partyID, err := sp.GetDSOPartyID(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, partyID)
	require.Contains(t, partyID, "DSO::")
}

func TestGetDSO_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sp := newTestScanProxy(t)

	dso, err := sp.GetDSO(ctx)
	if err != nil && strings.Contains(err.Error(), "502") {
		t.Skip("DSO endpoint returned 502, skipping")
	}
	require.NoError(t, err)
	require.NotNil(t, dso)
	require.NotEmpty(t, dso.ContractID)
	require.NotEmpty(t, dso.TemplateID)
	require.NotNil(t, dso.Payload)
}

func TestListANSEntries_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sp := newTestScanProxy(t)

	entries, err := sp.ListANSEntries(ctx)
	require.NoError(t, err)
	require.NotNil(t, entries)

	for i, entry := range entries {
		require.NotEmpty(t, entry.ContractID, "entry[%d] contract_id empty", i)
		require.NotEmpty(t, entry.TemplateID, "entry[%d] template_id empty", i)
		require.NotNil(t, entry.Payload, "entry[%d] payload nil", i)
	}
}

func TestGetANSEntryByName_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sp := newTestScanProxy(t)

	entries, err := sp.ListANSEntries(ctx)
	require.NoError(t, err)

	if len(entries) == 0 {
		t.Skip("no ANS entries on devnet, skipping")
	}

	name, ok := entries[0].Payload["name"].(string)
	require.True(t, ok, "first entry missing 'name' string")

	entry, err := sp.GetANSEntryByName(ctx, name)
	require.NoError(t, err)
	require.NotNil(t, entry)
	require.NotEmpty(t, entry.ContractID)
	require.Equal(t, entries[0].ContractID, entry.ContractID)
}

func TestGetANSRules_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sp := newTestScanProxy(t)

	rules, err := sp.GetANSRules(ctx)
	require.NoError(t, err)
	require.NotNil(t, rules)
	require.NotEmpty(t, rules.ContractID)
	require.NotEmpty(t, rules.TemplateID)
	require.NotNil(t, rules.Payload)
}
