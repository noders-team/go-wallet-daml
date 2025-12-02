package codegen_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/noders-team/go-daml/pkg/client"
	"github.com/noders-team/go-daml/pkg/model"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/rs/zerolog/log"
)

var cl *client.DamlBindingClient

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()

	dockerPool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatal().Err(err).Msg("Could not connect to docker")
	}

	if err := dockerPool.Client.Ping(); err != nil {
		log.Fatal().Err(err).Msg("Could not ping docker")
	}

	resDaml, grpcAddr := initDamlSandbox(ctx, dockerPool)

	builder := client.NewDamlClient("", grpcAddr)
	if strings.HasSuffix(grpcAddr, ":443") {
		tlsConfig := client.TlsConfig{}
		builder = builder.WithTLSConfig(tlsConfig)
	}

	cl, err = builder.Build(context.Background())
	if err != nil {
		log.Fatal().Err(err).Msg("failed to build DAML client")
	}

	log.Info().Msg("Canton sandbox initialization complete, setting up test environment")

	testUser := "app-provider"
	users, err := cl.UserMng.ListUsers(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to list users")
	}

	userExists := false
	for _, u := range users {
		log.Info().Msgf("existing user: %s, primary party: %s", u.ID, u.PrimaryParty)
		if u.ID == testUser {
			userExists = true
			log.Info().Msgf("user %s already exists", testUser)
		}
	}

	if !userExists {
		log.Info().Msgf("creating user %s", testUser)

		log.Info().Msg("waiting for synchronizer connection before allocating party...")
		time.Sleep(30 * time.Second)

		partyDetails, err := cl.PartyMng.AllocateParty(ctx, "", nil, "")
		if err != nil {
			log.Fatal().Err(err).Msg("failed to allocate party")
		}
		log.Info().Msgf("allocated party: %s", partyDetails.Party)

		user := &model.User{
			ID:           testUser,
			PrimaryParty: partyDetails.Party,
		}
		rights := []*model.Right{
			{Type: model.CanActAs{Party: partyDetails.Party}},
			{Type: model.CanReadAs{Party: partyDetails.Party}},
		}
		_, err = cl.UserMng.CreateUser(ctx, user, rights)
		if err != nil {
			log.Fatal().Err(err).Msgf("failed to create user %s", testUser)
		}
		log.Info().Msgf("created user %s with party %s", testUser, partyDetails.Party)
	}

	log.Info().Msg("Test environment ready, running tests")

	code := m.Run()

	purgeResources(dockerPool, resDaml)

	os.Exit(code)
}

func initDamlSandbox(ctx context.Context, dockerPool *dockertest.Pool) (*dockertest.Resource, string) {
	ledgerAPIPort := "6865"

	cantonConfig := `canton {
  mediators {
    mediator1 {
      admin-api.port = 6869
    }
  }
  sequencers {
    sequencer1 {
      admin-api.port = 6868
      public-api.port = 6867
      sequencer {
        type = reference
        config.storage.type = memory
      }
      storage.type = memory
    }
  }
  participants {
    sandbox {
      storage.type = memory
      admin-api.port = 6866
      ledger-api {
        address = "0.0.0.0"
        port = 6865
        user-management-service.enabled = true
      }
    }
  }
}
`

	tmpDir, err := os.MkdirTemp("", "canton-config-*")
	if err != nil {
		log.Fatal().Err(err).Msg("Could not create temp dir for Canton config")
	}

	configPath := fmt.Sprintf("%s/canton.conf", tmpDir)
	if err := os.WriteFile(configPath, []byte(cantonConfig), 0o644); err != nil {
		log.Fatal().Err(err).Msg("Could not write Canton config")
	}

	resource, err := dockerPool.RunWithOptions(&dockertest.RunOptions{
		Repository: "digitalasset/daml-sdk",
		Tag:        "3.5.0-snapshot.20251106.0",
		Platform:   "linux/amd64",
		Cmd: []string{
			"daml",
			"sandbox",
			"-c", "/canton/canton.conf",
		},
		ExposedPorts: []string{ledgerAPIPort + "/tcp"},
		Mounts:       []string{fmt.Sprintf("%s:/canton/canton.conf:ro", configPath)},
	}, func(config *docker.HostConfig) {
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{
			Name: "no",
		}
	})
	if err != nil {
		log.Fatal().Err(err).Msg("Could not start DAML sandbox")
	}

	resource.Expire(300)

	mappedLedgerPort := resource.GetPort(ledgerAPIPort + "/tcp")
	grpcAddr := fmt.Sprintf("127.0.0.1:%s", mappedLedgerPort)

	log.Info().Msgf("DAML sandbox started, Ledger API (gRPC) on %s", grpcAddr)

	if err := waitForPort(ctx, mappedLedgerPort, 2*time.Minute); err != nil {
		log.Fatal().Err(err).Msgf("DAML sandbox Ledger API port %s not ready", mappedLedgerPort)
	}

	log.Info().Msg("Port is open, waiting for Canton to fully initialize gRPC...")
	time.Sleep(120 * time.Second)

	return resource, grpcAddr
}

func waitForPort(ctx context.Context, port string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	address := fmt.Sprintf("127.0.0.1:%s", port)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		conn, err := net.DialTimeout("tcp", address, 1*time.Second)
		if err == nil {
			conn.Close()
			log.Info().Msgf("Port %s is ready", port)
			return nil
		}

		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for port %s", port)
}

func purgeResources(dockerPool *dockertest.Pool, resources ...*dockertest.Resource) {
	for i := range resources {
		if err := dockerPool.Purge(resources[i]); err != nil {
			log.Err(err).Msgf("Could not purge resource: %s", err)
		}
	}
}
