package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/noders-team/go-wallet-daml/pkg/auth"
	"github.com/rs/zerolog"
)

type HTTPClient struct {
	baseURL       string
	client        *http.Client
	tokenProvider *auth.AuthTokenProvider
	logger        zerolog.Logger
}

func NewHTTPClient(baseURL string, provider *auth.AuthTokenProvider, logger zerolog.Logger) *HTTPClient {
	return &HTTPClient{
		baseURL:       baseURL,
		client:        &http.Client{Timeout: 30 * time.Second},
		tokenProvider: provider,
		logger:        logger.With().Str("component", "http-client").Logger(),
	}
}

func (h *HTTPClient) Post(ctx context.Context, path string, body interface{}, result interface{}) error {
	jsonData, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", h.baseURL+path, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	token, err := h.tokenProvider.GetUserAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get access token: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	h.logger.Debug().Str("url", h.baseURL+path).Msg("HTTP POST request")

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		h.logger.Error().
			Int("status", resp.StatusCode).
			Str("body", string(bodyBytes)).
			Msg("HTTP error response")
		return fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

func (h *HTTPClient) Get(ctx context.Context, path string, result interface{}) error {
	req, err := http.NewRequestWithContext(ctx, "GET", h.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	token, err := h.tokenProvider.GetUserAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get access token: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)

	h.logger.Debug().Str("url", h.baseURL+path).Msg("HTTP GET request")

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		h.logger.Error().
			Int("status", resp.StatusCode).
			Str("body", string(bodyBytes)).
			Msg("HTTP error response")
		return fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}
