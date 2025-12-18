package sdk_test

import (
	"context"
	"os"
	"testing"

	"github.com/noders-team/go-daml/pkg/client"
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

func GetDamlClient() *client.DamlBindingClient {
	return testutil.GetClient()
}

func GetGrpcAddr() string {
	return testutil.GetGrpcAddr()
}

func GetScanProxyBaseURL() string {
	return testutil.GetScanProxyBaseURL()
}

func GetSynchronizerID() string {
	syncID := testutil.GetSynchronizerID()
	return string(syncID)
}
