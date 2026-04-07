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

func TestGetOpenMiningRounds_Cache(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sp := newTestScanProxy(t)

	rounds1, err := sp.GetOpenMiningRounds(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, rounds1)

	rounds2, err := sp.GetOpenMiningRounds(ctx)
	require.NoError(t, err)
	require.Equal(t, len(rounds1), len(rounds2))
	require.Equal(t, rounds1[0].ContractID, rounds2[0].ContractID)

	sp.InvalidateOpenMiningRoundsCache()

	rounds3, err := sp.GetOpenMiningRounds(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, rounds3)
	require.NotEmpty(t, rounds3[0].ContractID)
}
