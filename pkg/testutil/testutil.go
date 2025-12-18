package testutil

import (
	"context"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/noders-team/go-daml/pkg/client"
	damlModel "github.com/noders-team/go-daml/pkg/model"
	"github.com/noders-team/go-daml/pkg/types"
	"github.com/noders-team/go-wallet-daml/pkg/auth"
	"github.com/noders-team/go-wallet-daml/pkg/controller"
	"github.com/noders-team/go-wallet-daml/pkg/model"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/rs/zerolog/log"
)

const (
	damlSandboxVersion  = "3.5.0-snapshot.20251106.0"
	containerName       = "go-wallet-daml-test-canton"
	containerLabelKey   = "go-wallet-daml-test"
	containerLabelValue = "canton-sandbox"
	testUserID          = "app-provider"
)

var (
	once                      sync.Once
	setupErr                  error
	damlClient                *client.DamlBindingClient
	ledgerCtrl                *controller.LedgerController
	tokenStdCtrl              *controller.TokenStandardController
	validatorCtrl             *controller.ValidatorController
	authProvider              *auth.AuthTokenProvider
	testPartyID               model.PartyID
	synchronizerID            model.PartyID
	dsoPartyID                model.PartyID
	amuletRulesContractID     string
	amuletRulesTemplateID     string
	openMiningRoundContractID string
	scanProxyBaseURL          string
	dockerPool                *dockertest.Pool
	resDaml                   *dockertest.Resource
	grpcAddr                  string
	adminAddr                 string
)

func Setup(ctx context.Context) error {
	once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
		defer cancel()

		dockerPool, err := dockertest.NewPool("")
		if err != nil {
			setupErr = fmt.Errorf("could not connect to docker: %w", err)
			return
		}

		if err := dockerPool.Client.Ping(); err != nil {
			setupErr = fmt.Errorf("could not ping docker: %w", err)
			return
		}

		resDaml, grpcAddr, adminAddr = initDamlSandbox(ctx, dockerPool)

		builder := client.NewDamlClient("", grpcAddr).WithAdminAddress(adminAddr)
		if strings.HasSuffix(grpcAddr, ":443") {
			tlsConfig := client.TlsConfig{}
			builder = builder.WithTLSConfig(tlsConfig)
		}

		damlClient, err = builder.Build(context.Background())
		if err != nil {
			setupErr = fmt.Errorf("failed to build DAML client: %w", err)
			return
		}

		log.Info().Msg("Canton sandbox initialization complete, setting up test environment")

		users, err := damlClient.UserMng.ListUsers(ctx)
		if err != nil {
			setupErr = fmt.Errorf("failed to list users: %w", err)
			return
		}

		userExists := false
		var primaryParty string
		for _, u := range users {
			log.Info().Msgf("existing user: %s, primary party: %s", u.ID, u.PrimaryParty)
			if u.ID == testUserID {
				userExists = true
				primaryParty = u.PrimaryParty
				log.Info().Msgf("user %s already exists", testUserID)
			}
		}

		if !userExists {
			log.Info().Msgf("creating user %s", testUserID)

			log.Info().Msg("waiting for synchronizer connection before allocating party...")
			if err := waitForSynchronizerConnection(ctx, damlClient, 2*time.Minute); err != nil {
				setupErr = fmt.Errorf("synchronizer connection timeout: %w", err)
				return
			}

			partyDetails, err := damlClient.PartyMng.AllocateParty(ctx, "", nil, "")
			if err != nil {
				setupErr = fmt.Errorf("failed to allocate party: %w", err)
				return
			}
			log.Info().Msgf("allocated party: %s", partyDetails.Party)

			user := &damlModel.User{
				ID:           testUserID,
				PrimaryParty: partyDetails.Party,
			}
			rights := []*damlModel.Right{
				{Type: damlModel.CanActAs{Party: partyDetails.Party}},
				{Type: damlModel.CanReadAs{Party: partyDetails.Party}},
			}
			_, err = damlClient.UserMng.CreateUser(ctx, user, rights)
			if err != nil {
				setupErr = fmt.Errorf("failed to create user %s: %w", testUserID, err)
				return
			}
			log.Info().Msgf("created user %s with party %s", testUserID, partyDetails.Party)
			primaryParty = partyDetails.Party
		}

		testPartyID = model.PartyID(primaryParty)
		log.Info().Str("partyID", string(testPartyID)).Msg("Using test party")

		syncResp, err := damlClient.StateService.GetConnectedSynchronizers(ctx, nil)
		if err != nil {
			setupErr = fmt.Errorf("failed to get connected synchronizers: %w", err)
			return
		}

		if len(syncResp.ConnectedSynchronizers) == 0 {
			setupErr = fmt.Errorf("no connected synchronizers found")
			return
		}

		synchronizerID = model.PartyID(syncResp.ConnectedSynchronizers[0].SynchronizerID)
		log.Info().Str("synchronizerID", string(synchronizerID)).Msg("Using synchronizer")

		scanProxyBaseURL = "http://localhost:5012"

		authProvider = auth.NewMockAuthTokenProvider(testUserID)

		ledgerCtrl, err = controller.NewLedgerController(testUserID, grpcAddr, scanProxyBaseURL, authProvider, false)
		if err != nil {
			setupErr = fmt.Errorf("failed to create ledger controller: %w", err)
			return
		}
		ledgerCtrl.SetPartyID(testPartyID)
		ledgerCtrl.SetSynchronizerID(synchronizerID)

		tokenStdCtrl, err = controller.NewTokenStandardController(testUserID, grpcAddr, authProvider, false)
		if err != nil {
			setupErr = fmt.Errorf("failed to create token standard controller: %w", err)
			return
		}
		tokenStdCtrl.SetPartyID(testPartyID)
		tokenStdCtrl.SetSynchronizerID(synchronizerID)

		validatorCtrl, err = controller.NewValidatorController(testUserID, grpcAddr, scanProxyBaseURL, authProvider)
		if err != nil {
			setupErr = fmt.Errorf("failed to create validator controller: %w", err)
			return
		}
		validatorCtrl.SetPartyID(testPartyID)
		validatorCtrl.SetSynchronizerID(synchronizerID)

		log.Info().Msg("Wallet test environment ready")

		if err := uploadDarFiles(ctx, damlClient); err != nil {
			setupErr = fmt.Errorf("failed to upload DAR files: %w", err)
			return
		}

		log.Info().Msg("DAR files uploaded successfully")

		allParties, err := damlClient.PartyMng.ListKnownParties(ctx, "", 1000, "")
		if err != nil {
			setupErr = fmt.Errorf("failed to list known parties: %w", err)
			return
		}

		var dsoParty string
		for _, party := range allParties.PartyDetails {
			if strings.HasPrefix(party.Party, "dso::") {
				dsoParty = party.Party
				log.Info().Str("dsoParty", dsoParty).Msg("Using existing DSO party")
				break
			}
		}

		if dsoParty == "" {
			dsoPartyResp, err := damlClient.PartyMng.AllocateParty(ctx, "dso", nil, "")
			if err != nil {
				setupErr = fmt.Errorf("failed to allocate DSO party: %w", err)
				return
			}
			dsoParty = dsoPartyResp.Party
			log.Info().Str("dsoParty", dsoParty).Msg("DSO party allocated")
		}

		dsoPartyID = model.PartyID(dsoParty)

		rights := []*damlModel.Right{
			{Type: damlModel.CanActAs{Party: dsoParty}},
			{Type: damlModel.CanReadAs{Party: dsoParty}},
		}
		_, err = damlClient.UserMng.GrantUserRights(ctx, testUserID, "", rights)
		if err != nil {
			setupErr = fmt.Errorf("failed to grant DSO rights to user: %w", err)
			return
		}

		if err := initializeAmuletRules(ctx, damlClient, dsoParty); err != nil {
			setupErr = fmt.Errorf("failed to initialize AmuletRules: %w", err)
			return
		}

		log.Info().Msg("AmuletRules initialized successfully")
	})
	return setupErr
}

func Teardown() {
	if dockerPool != nil {
		if resDaml != nil {
			log.Info().Str("container", containerName).Msg("Keeping Canton sandbox container for reuse")
		}
	}
}

func CleanupContainer() {
	if dockerPool != nil && resDaml != nil {
		if err := dockerPool.Purge(resDaml); err != nil {
			log.Error().Err(err).Msg("Could not purge Canton sandbox resource")
		} else {
			log.Info().Str("container", containerName).Msg("Removed Canton sandbox container")
		}
	}
}

func findExistingContainer(pool *dockertest.Pool) (*dockertest.Resource, error) {
	containers, err := pool.Client.ListContainers(docker.ListContainersOptions{
		All: true,
		Filters: map[string][]string{
			"name": {containerName},
		},
	})
	if err != nil {
		return nil, err
	}

	var matchedContainer *docker.APIContainers
	for i := range containers {
		for _, name := range containers[i].Names {
			if name == "/"+containerName || name == containerName {
				matchedContainer = &containers[i]
				break
			}
		}
		if matchedContainer != nil {
			break
		}
	}

	if matchedContainer == nil {
		return nil, fmt.Errorf("no existing container found")
	}

	if matchedContainer.State != "running" {
		log.Warn().Str("state", matchedContainer.State).Msg("Found container but it's not running, removing it")
		err := pool.Client.RemoveContainer(docker.RemoveContainerOptions{
			ID:    matchedContainer.ID,
			Force: true,
		})
		if err != nil {
			log.Warn().Err(err).Msg("Failed to remove stopped container")
		}
		return nil, fmt.Errorf("container not running, removed")
	}

	portMap := make(map[docker.Port][]docker.PortBinding)
	for _, p := range matchedContainer.Ports {
		port := docker.Port(fmt.Sprintf("%d/%s", p.PrivatePort, p.Type))
		portMap[port] = []docker.PortBinding{
			{
				HostIP:   p.IP,
				HostPort: fmt.Sprintf("%d", p.PublicPort),
			},
		}
	}

	resource := &dockertest.Resource{
		Container: &docker.Container{
			ID:    matchedContainer.ID,
			Name:  matchedContainer.Names[0],
			State: docker.State{Running: true},
			NetworkSettings: &docker.NetworkSettings{
				Ports: portMap,
			},
		},
	}

	return resource, nil
}

func initDamlSandbox(ctx context.Context, dockerPool *dockertest.Pool) (*dockertest.Resource, string, string) {
	ledgerAPIPort := "6865"
	adminAPIPort := "6866"

	existingResource, err := findExistingContainer(dockerPool)
	if err == nil && existingResource != nil {
		log.Info().Str("container", containerName).Msg("Reusing existing Canton sandbox container")

		mappedLedgerPort := existingResource.GetPort(ledgerAPIPort + "/tcp")
		grpcAddr := fmt.Sprintf("127.0.0.1:%s", mappedLedgerPort)

		mappedAdminPort := existingResource.GetPort(adminAPIPort + "/tcp")
		adminAddr := fmt.Sprintf("127.0.0.1:%s", mappedAdminPort)

		log.Info().Msgf("Reusing Canton sandbox, Ledger API (gRPC) on %s", grpcAddr)

		if err := waitForPort(ctx, mappedLedgerPort, 30*time.Second); err != nil {
			log.Warn().Err(err).Msg("Existing container not responsive, creating new one")
		} else {
			return existingResource, grpcAddr, adminAddr
		}
	}

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
      admin-api {
	    address = "0.0.0.0"
	  	port = 6866
	  }
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
		Tag:        damlSandboxVersion,
		Platform:   "linux/amd64",
		Name:       containerName,
		Cmd: []string{
			"daml",
			"sandbox",
			"-c", "/canton/canton.conf",
			"--debug",
		},
		ExposedPorts: []string{ledgerAPIPort + "/tcp", adminAPIPort + "/tcp"},
		Mounts:       []string{fmt.Sprintf("%s:/canton/canton.conf:ro", configPath)},
		Labels: map[string]string{
			containerLabelKey: containerLabelValue,
		},
	}, func(config *docker.HostConfig) {
		config.AutoRemove = false
		config.RestartPolicy = docker.RestartPolicy{
			Name: "no",
		}
	})
	if err != nil {
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "is already in use") {
			log.Warn().Err(err).Msg("Container already exists, attempting to reuse it")
			existingResource, findErr := findExistingContainer(dockerPool)
			if findErr == nil && existingResource != nil {
				mappedLedgerPort := existingResource.GetPort(ledgerAPIPort + "/tcp")
				grpcAddr := fmt.Sprintf("127.0.0.1:%s", mappedLedgerPort)
				mappedAdminPort := existingResource.GetPort(adminAPIPort + "/tcp")
				adminAddr := fmt.Sprintf("127.0.0.1:%s", mappedAdminPort)
				log.Info().Str("container", containerName).Msg("Successfully attached to existing container")
				return existingResource, grpcAddr, adminAddr
			}
			log.Fatal().Err(err).Msg("Container exists but could not attach to it")
		}
		log.Fatal().Err(err).Msg("Could not start DAML sandbox")
	}

	resource.Expire(600)

	mappedLedgerPort := resource.GetPort(ledgerAPIPort + "/tcp")
	grpcAddr := fmt.Sprintf("127.0.0.1:%s", mappedLedgerPort)

	log.Info().Msgf("DAML sandbox started, Ledger API (gRPC) on %s", grpcAddr)

	if err := waitForPort(ctx, mappedLedgerPort, 2*time.Minute); err != nil {
		log.Fatal().Err(err).Msgf("DAML sandbox Ledger API port %s not ready", mappedLedgerPort)
	}
	log.Info().Msgf("canton ledger API port %s is ready", adminAPIPort)

	adminAPIPort = resource.GetPort(adminAPIPort + "/tcp")
	adminAddr = fmt.Sprintf("127.0.0.1:%s", adminAPIPort)
	if err := waitForPort(ctx, adminAPIPort, 2*time.Minute); err != nil {
		log.Fatal().Err(err).Msgf("Canton admin API port %s not ready", adminAPIPort)
	}
	log.Info().Msgf("canton admin API port %s is ready", adminAPIPort)

	log.Info().Msg("port is open, waiting for Canton to fully initialize...")
	if err := waitForCantonReady(ctx, dockerPool, resource, 3*time.Minute); err != nil {
		log.Fatal().Err(err).Msg("Canton sandbox initialization timeout")
	}

	return resource, grpcAddr, adminAddr
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

func waitForCantonReady(ctx context.Context, pool *dockertest.Pool, resource *dockertest.Resource, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	readyMessage := "Successfully started all nodes"

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		buf := &logBuffer{}
		err := pool.Client.Logs(docker.LogsOptions{
			Container:    resource.Container.ID,
			OutputStream: buf,
			Stdout:       true,
			Stderr:       true,
			Tail:         "100",
		})
		if err == nil {
			if strings.Contains(buf.String(), readyMessage) {
				log.Info().Msg("Canton sandbox is ready")
				return nil
			}
		}

		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("sandbox timeout: Canton sandbox did not become ready within %v", timeout)
}

type logBuffer struct {
	data []byte
}

func (b *logBuffer) Write(p []byte) (n int, err error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *logBuffer) String() string {
	return string(b.data)
}

func waitForSynchronizerConnection(ctx context.Context, cl *client.DamlBindingClient, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := cl.StateService.GetConnectedSynchronizers(ctx, &damlModel.GetConnectedSynchronizersRequest{})
		if err == nil && resp != nil && len(resp.ConnectedSynchronizers) > 0 {
			log.Info().
				Int("count", len(resp.ConnectedSynchronizers)).
				Str("first_id", resp.ConnectedSynchronizers[0].SynchronizerID).
				Msg("synchronizer connection established")
			return nil
		}

		if err != nil {
			log.Debug().Err(err).Msg("checking for synchronizer connection")
		}

		time.Sleep(3 * time.Second)
	}

	return fmt.Errorf("no synchronizer connection after %v", timeout)
}

func GetLedgerController() *controller.LedgerController {
	return ledgerCtrl
}

func GetTokenStandardController() *controller.TokenStandardController {
	return tokenStdCtrl
}

func GetValidatorController() *controller.ValidatorController {
	return validatorCtrl
}

func GetAuthProvider() *auth.AuthTokenProvider {
	return authProvider
}

func GetTestPartyID() model.PartyID {
	return testPartyID
}

func GetSynchronizerID() model.PartyID {
	return synchronizerID
}

func GetDsoPartyID() model.PartyID {
	return dsoPartyID
}

func GetGrpcAddr() string {
	return grpcAddr
}

func GetAdminAddr() string {
	return adminAddr
}

func GetScanProxyBaseURL() string {
	return scanProxyBaseURL
}

func GetClient() *client.DamlBindingClient {
	return damlClient
}

func GetAmuletRulesContractID() string {
	return amuletRulesContractID
}

func GetAmuletRulesTemplateID() string {
	return amuletRulesTemplateID
}

func GetOpenMiningRoundContractID() string {
	return openMiningRoundContractID
}

func uploadDarFiles(ctx context.Context, cl *client.DamlBindingClient) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	darDir := filepath.Join(workDir, "..", "..", ".dar")
	absPath, err := filepath.Abs(darDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		log.Warn().Str("darDir", absPath).Msg("DAR directory not found, skipping DAR upload")
		return nil
	}

	darDir = absPath

	files, err := filepath.Glob(filepath.Join(darDir, "*.dar"))
	if err != nil {
		return fmt.Errorf("failed to list DAR files: %w", err)
	}

	if len(files) == 0 {
		log.Warn().Str("darDir", darDir).Msg("No DAR files found")
		return nil
	}

	existingPackages, err := cl.PackageMng.ListKnownPackages(ctx)
	if err != nil {
		return fmt.Errorf("failed to list existing packages: %w", err)
	}

	packageNames := make(map[string]bool)
	for _, pkg := range existingPackages {
		if pkg.Name != "" {
			packageNames[pkg.Name] = true
		}
	}

	for _, darFile := range files {
		fileName := filepath.Base(darFile)
		baseName := strings.TrimSuffix(fileName, ".dar")
		pkgName := strings.Split(baseName, "-")[0]

		if packageNames[pkgName] {
			log.Info().Str("package", pkgName).Str("file", fileName).Msg("Package already uploaded, skipping")
			continue
		}

		log.Info().Str("file", fileName).Msg("Uploading DAR file")
		darBytes, err := os.ReadFile(darFile)
		if err != nil {
			return fmt.Errorf("failed to read DAR file %s: %w", darFile, err)
		}

		submissionID := fmt.Sprintf("upload-dar-%s-%d", baseName, time.Now().Unix())
		if err := cl.PackageMng.UploadDarFile(ctx, darBytes, submissionID); err != nil {
			return fmt.Errorf("failed to upload DAR file %s: %w", darFile, err)
		}

		if err := cl.PackageMng.ValidateDarFile(ctx, darBytes, submissionID); err != nil {
			return fmt.Errorf("failed to validate DAR file %s: %w", darFile, err)
		}

		log.Info().Str("file", fileName).Msg("DAR file uploaded successfully")
	}

	return nil
}

func initializeAmuletRules(ctx context.Context, cl *client.DamlBindingClient, dsoParty string) error {
	var err error

	packages, err := cl.PackageMng.ListKnownPackages(ctx)
	if err != nil {
		return fmt.Errorf("failed to list packages: %w", err)
	}

	var spliceAmuletPkgID string
	for _, pkg := range packages {
		if pkg.Name == "splice-amulet" {
			spliceAmuletPkgID = pkg.PackageID
			log.Info().Str("packageID", spliceAmuletPkgID).Msg("Found splice-amulet package")
			break
		}
	}

	if spliceAmuletPkgID == "" {
		return fmt.Errorf("splice-amulet package not found")
	}

	amuletRulesTemplateID = fmt.Sprintf("%s:Splice.AmuletRules:AmuletRules", spliceAmuletPkgID)
	extAmuletRulesTemplateID := fmt.Sprintf("%s:Splice.ExternalPartyAmuletRules:ExternalPartyAmuletRules", spliceAmuletPkgID)

	// First, check if contract already exists
	req := &damlModel.GetActiveContractsRequest{
		EventFormat: &damlModel.EventFormat{
			Verbose: true,
			FiltersByParty: map[string]*damlModel.Filters{
				dsoParty: {
					Inclusive: &damlModel.InclusiveFilters{
						TemplateFilters: []*damlModel.TemplateFilter{
							{
								TemplateID:              amuletRulesTemplateID,
								IncludeCreatedEventBlob: false,
							},
						},
					},
				},
			},
		},
	}

	stream, errChan := cl.StateService.GetActiveContracts(ctx, req)

	var existingContractID string
checkLoop:
	for {
		select {
		case resp, ok := <-stream:
			if !ok {
				break checkLoop
			}
			if entry, ok := resp.ContractEntry.(*damlModel.ActiveContractEntry); ok {
				if entry.ActiveContract != nil && entry.ActiveContract.CreatedEvent != nil {
					existingContractID = entry.ActiveContract.CreatedEvent.ContractID
					log.Info().Str("contractID", existingContractID).Msg("AmuletRules contract already exists")
					break checkLoop
				}
			}
		case err := <-errChan:
			if err != nil {
				log.Debug().Err(err).Msg("Error checking for existing AmuletRules")
			}
		case <-time.After(2 * time.Second):
			break checkLoop
		}
	}

	if existingContractID != "" {
		log.Info().Msg("AmuletRules already initialized, skipping creation")
		return nil
	}

	log.Info().Msg("Creating AmuletRules contracts")

	decimalToNumeric := func(val float64) types.NUMERIC {
		scaled := int64(val * 10000000000)
		return types.NUMERIC(big.NewInt(scaled))
	}

	transferConfig := map[string]interface{}{
		"createFee":     map[string]interface{}{"fee": decimalToNumeric(0.03)},
		"holdingFee":    map[string]interface{}{"rate": decimalToNumeric(0.00002)},
		"lockHolderFee": map[string]interface{}{"fee": decimalToNumeric(0.005)},
		"transferFee": map[string]interface{}{
			"initialRate": decimalToNumeric(0.01),
			"steps": []interface{}{
				map[string]interface{}{"_1": decimalToNumeric(100.0), "_2": decimalToNumeric(0.001)},
				map[string]interface{}{"_1": decimalToNumeric(1000.0), "_2": decimalToNumeric(0.0001)},
				map[string]interface{}{"_1": decimalToNumeric(1000000.0), "_2": decimalToNumeric(0.00001)},
			},
		},
		"extraFeaturedAppRewardAmount": decimalToNumeric(1.0),
		"maxNumInputs":                 100,
		"maxNumOutputs":                100,
		"maxNumLockHolders":            10,
	}

	issuanceConfig := map[string]interface{}{
		"amuletToIssuePerYear":      decimalToNumeric(40000000000.0),
		"validatorRewardPercentage": decimalToNumeric(0.05),
		"appRewardPercentage":       decimalToNumeric(0.15),
		"validatorRewardCap":        decimalToNumeric(0.2),
		"featuredAppRewardCap":      decimalToNumeric(100.0),
		"unfeaturedAppRewardCap":    decimalToNumeric(0.6),
		"optValidatorFaucetCap":     map[string]interface{}{"_type": "optional"},
	}

	defaultSynchronizerFees := map[string]interface{}{
		"baseRateTrafficLimits": map[string]interface{}{
			"burstAmount": 200000,
			"burstWindow": map[string]interface{}{"microseconds": int64(600000000)},
		},
		"minTopupAmount":           1000000,
		"extraTrafficPrice":        decimalToNumeric(1.0),
		"readVsWriteScalingFactor": 4,
	}

	defaultDecentralizedSynchronizerConfig := map[string]interface{}{
		"requiredSynchronizers": map[string]interface{}{
			"map": map[string]interface{}{
				"_type": "genmap",
				"value": map[string]interface{}{
					"decentralized-synchronizer-id-0": map[string]interface{}{"_type": "unit"},
				},
			},
		},
		"activeSynchronizer": "decentralized-synchronizer-id-0",
		"fees":               defaultSynchronizerFees,
	}

	amuletConfig := map[string]interface{}{
		"transferConfig": transferConfig,
		"issuanceCurve": map[string]interface{}{
			"initialValue": issuanceConfig,
			"futureValues": []interface{}{},
		},
		"decentralizedSynchronizer": defaultDecentralizedSynchronizerConfig,
		"tickDuration":              map[string]interface{}{"microseconds": int64(1000000)},
		"packageConfig": map[string]interface{}{
			"amulet":             "0.1.0",
			"amuletNameService":  "0.1.0",
			"dsoGovernance":      "0.1.0",
			"validatorLifecycle": "0.1.0",
			"wallet":             "0.1.0",
			"walletPayments":     "0.1.0",
		},
		"transferPreapprovalFee":          map[string]interface{}{"_type": "optional", "value": decimalToNumeric(0.0027397260273972603)},
		"featuredAppActivityMarkerAmount": map[string]interface{}{"_type": "optional", "value": decimalToNumeric(1.0)},
	}

	configSchedule := map[string]interface{}{
		"initialValue": amuletConfig,
		"futureValues": []interface{}{},
	}

	amuletRulesArgs := map[string]interface{}{
		"dso":            types.PARTY(dsoParty),
		"configSchedule": configSchedule,
		"isDevNet":       true,
	}

	amuletRulesCmd := &damlModel.Command{
		Command: &damlModel.CreateCommand{
			TemplateID: amuletRulesTemplateID,
			Arguments:  amuletRulesArgs,
		},
	}

	submissionID := fmt.Sprintf("create-amulet-rules-%d", time.Now().Unix())
	submitReq := &damlModel.SubmitAndWaitRequest{
		Commands: &damlModel.Commands{
			UserID:    testUserID,
			ActAs:     []string{dsoParty},
			CommandID: submissionID,
			Commands:  []*damlModel.Command{amuletRulesCmd},
		},
	}

	amuletRulesResp, err := cl.CommandService.SubmitAndWait(ctx, submitReq)
	if err != nil {
		return fmt.Errorf("failed to create AmuletRules: %w", err)
	}

	log.Info().
		Str("updateID", amuletRulesResp.UpdateID).
		Int64("completionOffset", amuletRulesResp.CompletionOffset).
		Msg("AmuletRules contract created successfully")

	getUpdatesReq := &damlModel.GetUpdatesRequest{
		BeginExclusive: amuletRulesResp.CompletionOffset - 1,
		EndInclusive:   &amuletRulesResp.CompletionOffset,
		UpdateFormat: &damlModel.EventFormat{
			Verbose: true,
			FiltersByParty: map[string]*damlModel.Filters{
				dsoParty: {
					Inclusive: &damlModel.InclusiveFilters{},
				},
			},
		},
	}

	updatesStream, updatesErrChan := cl.UpdateService.GetUpdates(ctx, getUpdatesReq)

updatesLoop:
	for {
		select {
		case resp, ok := <-updatesStream:
			if !ok {
				break updatesLoop
			}
			if resp.Update != nil && resp.Update.Transaction != nil {
				tx := resp.Update.Transaction
				log.Info().
					Str("updateID", tx.UpdateID).
					Str("commandID", tx.CommandID).
					Int("eventCount", len(tx.Events)).
					Msg("Transaction found")

				for i, event := range tx.Events {
					if event.Created != nil {
						log.Info().
							Int("eventIndex", i).
							Str("contractID", event.Created.ContractID).
							Str("templateID", event.Created.TemplateID).
							Str("signatories", fmt.Sprintf("%v", event.Created.Signatories)).
							Str("observers", fmt.Sprintf("%v", event.Created.Observers)).
							Interface("arguments", event.Created.CreateArguments).
							Msg("Contract created in transaction")

						if event.Created.TemplateID == amuletRulesTemplateID {
							amuletRulesContractID = event.Created.ContractID
							log.Info().
								Str("contractID", amuletRulesContractID).
								Str("templateID", amuletRulesTemplateID).
								Msg("Stored AmuletRules contract ID for later use")

							os.Setenv("AMULET_RULES_TEMPLATE_ID", amuletRulesTemplateID)
							os.Setenv("AMULET_RULES_CONTRACT_ID", amuletRulesContractID)
						}
					} else if event.Archived != nil {
						log.Info().
							Int("eventIndex", i).
							Str("contractID", event.Archived.ContractID).
							Str("templateID", event.Archived.TemplateID).
							Msg("Contract archived in transaction")
					}
				}
			}
		case err := <-updatesErrChan:
			if err != nil {
				log.Warn().Err(err).Msg("Error getting updates")
				break updatesLoop
			}
		case <-time.After(2 * time.Second):
			break updatesLoop
		}
	}

	extAmuletRulesArgs := map[string]interface{}{
		"dso": types.PARTY(dsoParty),
	}

	extAmuletRulesCmd := &damlModel.Command{
		Command: &damlModel.CreateCommand{
			TemplateID: extAmuletRulesTemplateID,
			Arguments:  extAmuletRulesArgs,
		},
	}

	extSubmissionID := fmt.Sprintf("create-ext-amulet-rules-%d", time.Now().Unix())
	extSubmitReq := &damlModel.SubmitAndWaitRequest{
		Commands: &damlModel.Commands{
			UserID:    testUserID,
			ActAs:     []string{dsoParty},
			CommandID: extSubmissionID,
			Commands:  []*damlModel.Command{extAmuletRulesCmd},
		},
	}

	_, err = cl.CommandService.SubmitAndWait(ctx, extSubmitReq)
	if err != nil {
		return fmt.Errorf("failed to create ExternalPartyAmuletRules: %w", err)
	}

	log.Info().Msg("ExternalPartyAmuletRules contract created")

	time.Sleep(2 * time.Second)

	contractFound := false
	contractID, err := getContractIDByTemplateID(ctx, cl, dsoParty, amuletRulesTemplateID)
	if err == nil {
		contractFound = true
	}

	if !contractFound {
		log.Warn().
			Str("dsoParty", dsoParty).
			Str("templateID", amuletRulesTemplateID).
			Msg("Contract not found with DSO party filter - trying query with participant_admin")

		allPartiesResp, err := cl.PartyMng.ListKnownParties(ctx, "", 1000, "")
		if err != nil {
			log.Error().Err(err).Msg("Failed to list known parties")
		} else {
			log.Info().Int("partyCount", len(allPartiesResp.PartyDetails)).Msg("Total known parties")

			for _, p := range allPartiesResp.PartyDetails {
				log.Debug().
					Str("party", p.Party).
					Bool("isLocal", p.IsLocal).
					Msg("known party")
				contractIDFound, err := getContractIDByTemplateID(ctx, cl, p.Party, amuletRulesTemplateID)
				if err != nil {
					continue
				}
				contractID = contractIDFound
			}
		}
	}

	log.Info().
		Str("dsoParty", dsoParty).
		Str("amuletRulesTemplateID", amuletRulesTemplateID).
		Bool("contractFound", contractFound).
		Str("contractID", contractID).
		Msg("AmuletRules contracts initialized")

	log.Info().Msg("Bootstrapping OpenMiningRound contracts")

	bootstrapArgs := map[string]interface{}{
		"amuletPrice":    decimalToNumeric(1.0),
		"round0Duration": map[string]interface{}{"microseconds": int64(1000000)},
		"initialRound":   map[string]interface{}{"_type": "optional", "value": 0},
	}

	bootstrapCmd := &damlModel.Command{
		Command: &damlModel.ExerciseCommand{
			TemplateID: amuletRulesTemplateID,
			ContractID: amuletRulesContractID,
			Choice:     "AmuletRules_Bootstrap_Rounds",
			Arguments:  bootstrapArgs,
		},
	}

	bootstrapSubmissionID := fmt.Sprintf("bootstrap-rounds-%d", time.Now().Unix())
	bootstrapReq := &damlModel.SubmitAndWaitRequest{
		Commands: &damlModel.Commands{
			UserID:    testUserID,
			ActAs:     []string{dsoParty},
			CommandID: bootstrapSubmissionID,
			Commands:  []*damlModel.Command{bootstrapCmd},
		},
	}

	bootstrapResp, err := cl.CommandService.SubmitAndWait(ctx, bootstrapReq)
	if err != nil {
		return fmt.Errorf("failed to bootstrap rounds: %w", err)
	}

	log.Info().
		Str("updateID", bootstrapResp.UpdateID).
		Int64("completionOffset", bootstrapResp.CompletionOffset).
		Msg("Bootstrap rounds command submitted successfully")

	bootstrapUpdatesReq := &damlModel.GetUpdatesRequest{
		BeginExclusive: bootstrapResp.CompletionOffset - 1,
		EndInclusive:   &bootstrapResp.CompletionOffset,
		UpdateFormat: &damlModel.EventFormat{
			Verbose: true,
			FiltersByParty: map[string]*damlModel.Filters{
				dsoParty: {
					Inclusive: &damlModel.InclusiveFilters{},
				},
			},
		},
	}

	bootstrapUpdatesStream, bootstrapUpdatesErrChan := cl.UpdateService.GetUpdates(ctx, bootstrapUpdatesReq)

	openMiningRoundTemplateID := fmt.Sprintf("%s:Splice.Round:OpenMiningRound", spliceAmuletPkgID)

bootstrapLoop:
	for {
		select {
		case resp, ok := <-bootstrapUpdatesStream:
			if !ok {
				break bootstrapLoop
			}
			if resp.Update != nil && resp.Update.Transaction != nil {
				tx := resp.Update.Transaction
				for _, event := range tx.Events {
					if event.Created != nil && event.Created.TemplateID == openMiningRoundTemplateID {
						openMiningRoundContractID = event.Created.ContractID
						log.Info().
							Str("contractID", openMiningRoundContractID).
							Str("templateID", openMiningRoundTemplateID).
							Msg("Stored OpenMiningRound contract ID for later use")
						os.Setenv("OPEN_MINING_ROUND_CONTRACT_ID", openMiningRoundContractID)
					}
				}
			}
		case err := <-bootstrapUpdatesErrChan:
			if err != nil {
				log.Warn().Err(err).Msg("Error getting bootstrap updates")
				break bootstrapLoop
			}
		case <-time.After(2 * time.Second):
			break bootstrapLoop
		}
	}

	if openMiningRoundContractID == "" {
		return fmt.Errorf("failed to extract OpenMiningRound contract ID from bootstrap transaction")
	}

	log.Info().Str("openMiningRoundCid", openMiningRoundContractID).Msg("OpenMiningRound bootstrapped successfully")

	return nil
}

func getContractIDByTemplateID(ctx context.Context, cl *client.DamlBindingClient, party, templateID string) (string, error) {
	verifyReq := &damlModel.GetActiveContractsRequest{
		EventFormat: &damlModel.EventFormat{
			Verbose: true,
			FiltersByParty: map[string]*damlModel.Filters{
				party: {
					Inclusive: &damlModel.InclusiveFilters{
						TemplateFilters: []*damlModel.TemplateFilter{
							{
								TemplateID:              templateID,
								IncludeCreatedEventBlob: false,
							},
						},
					},
				},
			},
		},
	}

	streamCh, errCh := cl.StateService.GetActiveContracts(ctx, verifyReq)

	for {
		select {
		case resp, ok := <-streamCh:
			if !ok {
				return "", fmt.Errorf("contract not found")
			}
			if entry, ok := resp.ContractEntry.(*damlModel.ActiveContractEntry); ok {
				if entry.ActiveContract != nil && entry.ActiveContract.CreatedEvent != nil {
					contractID := entry.ActiveContract.CreatedEvent.ContractID
					log.Info().
						Str("contractID", contractID).
						Str("templateID", entry.ActiveContract.CreatedEvent.TemplateID).
						Msg("verified contract exists")
					return contractID, nil
				}
			}
		case err := <-errCh:
			if err != nil {
				log.Warn().Err(err).Msg("error verifying contract")
				return "", err
			}
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(3 * time.Second):
			return "", fmt.Errorf("timeout waiting for contract")
		}
	}
}
