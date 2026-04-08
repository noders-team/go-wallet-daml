package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/noders-team/go-wallet-daml/pkg/auth"
	"github.com/noders-team/go-wallet-daml/pkg/model"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type ScanProxyClient struct {
	baseURL       string
	httpClient    *http.Client
	tokenProvider *auth.AuthTokenProvider
	isAdmin       bool
	logger        zerolog.Logger
}

type Contract struct {
	ContractID       string                 `json:"contract_id"`
	TemplateID       string                 `json:"template_id"`
	Payload          map[string]interface{} `json:"payload"`
	CreatedEventBlob string                 `json:"created_event_blob,omitempty"`
	CreatedAt        string                 `json:"created_at,omitempty"`
}

type ContractEntry struct {
	Contract *Contract `json:"contract"`
	DomainID string    `json:"domain_id"`
}

type AmuletRulesResponse struct {
	AmuletRules *ContractEntry `json:"amulet_rules"`
}

type OpenMiningRoundsResponse struct {
	OpenRounds    []ContractEntry `json:"open_mining_rounds"`
	IssuingRounds []ContractEntry `json:"issuing_mining_rounds"`
}

type TransferPreapprovalResponse struct {
	TransferPreapproval *ContractEntry `json:"transfer_preapproval"`
}

type FeaturedAppResponse struct {
	FeaturedAppRight *ContractEntry `json:"featured_app_right"`
}

type DSOPartyIDResponse struct {
	DSOPartyID string `json:"dso_party_id"`
}

type DSOResponse struct {
	DSO *ContractEntry `json:"dso"`
}

type ANSEntryResponse struct {
	ANSEntry *ContractEntry `json:"ans_entry"`
}

type ANSEntriesResponse struct {
	ANSEntries []ContractEntry `json:"ans_entries"`
}

type TransferCommandCounterResponse struct {
	Counter int64 `json:"counter"`
}

type ANSRulesResponse struct {
	ANSRules *ContractEntry `json:"ans_rules"`
}

func NewScanProxyClient(baseURL string, provider *auth.AuthTokenProvider, isAdmin bool) *ScanProxyClient {
	logger := log.Logger.With().Str("component", "scan-proxy-client").Logger()

	return &ScanProxyClient{
		baseURL:       baseURL,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		tokenProvider: provider,
		isAdmin:       isAdmin,
		logger:        logger,
	}
}

func (s *ScanProxyClient) get(ctx context.Context, path string, result interface{}) error {
	req, err := http.NewRequestWithContext(ctx, "GET", s.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	var token string
	if s.isAdmin {
		token, err = s.tokenProvider.GetAdminAccessToken(ctx)
	} else {
		token, err = s.tokenProvider.GetUserAccessToken(ctx)
	}
	if err != nil {
		return fmt.Errorf("failed to get access token: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	s.logger.Debug().Str("url", s.baseURL+path).Msg("Scan-proxy GET request")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		s.logger.Error().
			Int("status", resp.StatusCode).
			Str("body", string(bodyBytes)).
			Msg("Scan-proxy error response")
		return fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

func (s *ScanProxyClient) post(ctx context.Context, path string, body interface{}, result interface{}) error {
	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	var token string
	if s.isAdmin {
		token, err = s.tokenProvider.GetAdminAccessToken(ctx)
	} else {
		token, err = s.tokenProvider.GetUserAccessToken(ctx)
	}
	if err != nil {
		return fmt.Errorf("failed to get access token: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	s.logger.Debug().Str("url", s.baseURL+path).Msg("Scan-proxy POST request")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		s.logger.Error().
			Int("status", resp.StatusCode).
			Str("body", string(bodyBytes)).
			Msg("Scan-proxy error response")
		return fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

func (s *ScanProxyClient) GetAmuletRules(ctx context.Context) (*Contract, error) {
	var resp AmuletRulesResponse
	if err := s.get(ctx, "/v0/scan-proxy/amulet-rules", &resp); err != nil {
		return nil, fmt.Errorf("failed to get amulet rules: %w", err)
	}

	if resp.AmuletRules == nil || resp.AmuletRules.Contract == nil {
		return nil, fmt.Errorf("amulet rules not found in response")
	}

	contract := resp.AmuletRules.Contract
	if contract.ContractID == "" || contract.TemplateID == "" {
		return nil, fmt.Errorf("invalid amulet rules contract structure")
	}

	return contract, nil
}

func (s *ScanProxyClient) GetAmuletSynchronizerID(ctx context.Context) (string, error) {
	rules, err := s.GetAmuletRules(ctx)
	if err != nil {
		return "", err
	}

	if syncID, ok := rules.Payload["synchronizerId"].(string); ok {
		return syncID, nil
	}

	return "", fmt.Errorf("synchronizerId not found in amulet rules")
}

func (s *ScanProxyClient) GetOpenMiningRounds(ctx context.Context) ([]*Contract, error) {
	var resp OpenMiningRoundsResponse
	if err := s.get(ctx, "/v0/scan-proxy/open-and-issuing-mining-rounds", &resp); err != nil {
		return nil, fmt.Errorf("failed to get open mining rounds: %w", err)
	}

	contracts := make([]*Contract, 0, len(resp.OpenRounds))
	for i := range resp.OpenRounds {
		c := resp.OpenRounds[i].Contract
		if c == nil || c.ContractID == "" || c.TemplateID == "" {
			return nil, fmt.Errorf("invalid mining round contract structure at index %d", i)
		}
		contracts = append(contracts, c)
	}

	return contracts, nil
}

func (s *ScanProxyClient) GetActiveOpenMiningRound(ctx context.Context) (*Contract, error) {
	rounds, err := s.GetOpenMiningRounds(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	for _, round := range rounds {
		payload := round.Payload

		if opensAtStr, ok := payload["opensAt"].(string); ok {
			opensAt, err := time.Parse(time.RFC3339, opensAtStr)
			if err != nil {
				continue
			}

			if targetClosesAtStr, ok := payload["targetClosesAt"].(string); ok {
				targetClosesAt, err := time.Parse(time.RFC3339, targetClosesAtStr)
				if err != nil {
					continue
				}

				if now.After(opensAt) && now.Before(targetClosesAt) {
					return round, nil
				}
			}
		}
	}

	return nil, nil
}

func (s *ScanProxyClient) GetTransferPreApprovalByParty(ctx context.Context, party model.PartyID) (*Contract, error) {
	path := fmt.Sprintf("/v0/scan-proxy/transfer-preapprovals/by-party/%s", string(party))

	var resp TransferPreapprovalResponse
	if err := s.get(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("failed to get transfer preapproval: %w", err)
	}

	if resp.TransferPreapproval == nil || resp.TransferPreapproval.Contract == nil {
		return nil, nil
	}

	contract := resp.TransferPreapproval.Contract
	if contract.ContractID == "" || contract.TemplateID == "" {
		return nil, fmt.Errorf("invalid transfer preapproval contract structure")
	}

	return contract, nil
}

func (s *ScanProxyClient) GetFeaturedAppByProvider(ctx context.Context, providerParty model.PartyID) (*Contract, error) {
	path := fmt.Sprintf("/v0/scan-proxy/featured-apps/%s", string(providerParty))

	var resp FeaturedAppResponse
	if err := s.get(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("failed to get featured app: %w", err)
	}

	if resp.FeaturedAppRight == nil || resp.FeaturedAppRight.Contract == nil {
		return nil, nil
	}

	contract := resp.FeaturedAppRight.Contract
	if contract.ContractID == "" || contract.TemplateID == "" {
		return nil, fmt.Errorf("invalid featured app contract structure")
	}

	return contract, nil
}

func (s *ScanProxyClient) GetDSOPartyID(ctx context.Context) (string, error) {
	var resp DSOPartyIDResponse
	if err := s.get(ctx, "/v0/scan-proxy/dso-party-id", &resp); err != nil {
		return "", fmt.Errorf("failed to get DSO party ID: %w", err)
	}

	if resp.DSOPartyID == "" {
		return "", fmt.Errorf("DSO party ID not found in response")
	}

	return resp.DSOPartyID, nil
}

func (s *ScanProxyClient) GetDSO(ctx context.Context) (*Contract, error) {
	var resp DSOResponse
	if err := s.get(ctx, "/v0/scan-proxy/dso", &resp); err != nil {
		return nil, fmt.Errorf("failed to get DSO: %w", err)
	}

	if resp.DSO == nil || resp.DSO.Contract == nil {
		return nil, fmt.Errorf("DSO not found in response")
	}

	contract := resp.DSO.Contract
	if contract.ContractID == "" || contract.TemplateID == "" {
		return nil, fmt.Errorf("invalid DSO contract structure")
	}

	return contract, nil
}

func (s *ScanProxyClient) GetANSEntryByParty(ctx context.Context, party model.PartyID) (*Contract, error) {
	path := fmt.Sprintf("/v0/scan-proxy/ans-entries/by-party/%s", string(party))

	var resp ANSEntryResponse
	if err := s.get(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("failed to get ANS entry by party: %w", err)
	}

	if resp.ANSEntry == nil || resp.ANSEntry.Contract == nil {
		return nil, nil
	}

	contract := resp.ANSEntry.Contract
	if contract.ContractID == "" || contract.TemplateID == "" {
		return nil, fmt.Errorf("invalid ANS entry contract structure")
	}

	return contract, nil
}

func (s *ScanProxyClient) ListANSEntries(ctx context.Context) ([]*Contract, error) {
	var resp ANSEntriesResponse
	if err := s.get(ctx, "/v0/scan-proxy/ans-entries", &resp); err != nil {
		return nil, fmt.Errorf("failed to list ANS entries: %w", err)
	}

	contracts := make([]*Contract, 0, len(resp.ANSEntries))
	for i := range resp.ANSEntries {
		c := resp.ANSEntries[i].Contract
		if c == nil || c.ContractID == "" || c.TemplateID == "" {
			return nil, fmt.Errorf("invalid ANS entry contract structure at index %d", i)
		}
		contracts = append(contracts, c)
	}

	return contracts, nil
}

func (s *ScanProxyClient) GetANSEntryByName(ctx context.Context, name string) (*Contract, error) {
	path := fmt.Sprintf("/v0/scan-proxy/ans-entries/by-name/%s", name)

	var resp ANSEntryResponse
	if err := s.get(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("failed to get ANS entry by name: %w", err)
	}

	if resp.ANSEntry == nil || resp.ANSEntry.Contract == nil {
		return nil, nil
	}

	contract := resp.ANSEntry.Contract
	if contract.ContractID == "" || contract.TemplateID == "" {
		return nil, fmt.Errorf("invalid ANS entry contract structure")
	}

	return contract, nil
}

func (s *ScanProxyClient) GetTransferCommandCounter(ctx context.Context, party model.PartyID) (int64, error) {
	path := fmt.Sprintf("/v0/scan-proxy/transfer-command-counter/%s", string(party))

	var resp TransferCommandCounterResponse
	if err := s.get(ctx, path, &resp); err != nil {
		return 0, fmt.Errorf("failed to get transfer command counter: %w", err)
	}

	return resp.Counter, nil
}

func (s *ScanProxyClient) GetANSRules(ctx context.Context) (*Contract, error) {
	var resp ANSRulesResponse
	if err := s.post(ctx, "/v0/scan-proxy/ans-rules", nil, &resp); err != nil {
		return nil, fmt.Errorf("failed to get ANS rules: %w", err)
	}

	if resp.ANSRules == nil || resp.ANSRules.Contract == nil {
		return nil, fmt.Errorf("ANS rules not found in response")
	}

	contract := resp.ANSRules.Contract
	if contract.ContractID == "" || contract.TemplateID == "" {
		return nil, fmt.Errorf("invalid ANS rules contract structure")
	}

	return contract, nil
}
