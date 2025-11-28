package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/noders-team/go-wallet-daml/pkg/model"
	"github.com/rs/zerolog"
)

type ClientCredentialOAuthController struct {
	configURL   string
	userID      string
	userSecret  string
	adminID     string
	adminSecret string
	scope       string
	audience    string
	logger      zerolog.Logger

	tokenEndpoint string
	initOnce      sync.Once
	initErr       error

	accessTokens sync.Map
	pendingReqs  sync.Map
}

type OAuthOption func(*ClientCredentialOAuthController)

func WithUserID(userID string) OAuthOption {
	return func(c *ClientCredentialOAuthController) {
		c.userID = userID
	}
}

func WithUserSecret(secret string) OAuthOption {
	return func(c *ClientCredentialOAuthController) {
		c.userSecret = secret
	}
}

func WithAdminID(adminID string) OAuthOption {
	return func(c *ClientCredentialOAuthController) {
		c.adminID = adminID
	}
}

func WithAdminSecret(secret string) OAuthOption {
	return func(c *ClientCredentialOAuthController) {
		c.adminSecret = secret
	}
}

func WithScope(scope string) OAuthOption {
	return func(c *ClientCredentialOAuthController) {
		c.scope = scope
	}
}

func WithAudience(audience string) OAuthOption {
	return func(c *ClientCredentialOAuthController) {
		c.audience = audience
	}
}

func WithLogger(logger zerolog.Logger) OAuthOption {
	return func(c *ClientCredentialOAuthController) {
		c.logger = logger
	}
}

func NewClientCredentialOAuthController(configURL string, opts ...OAuthOption) *ClientCredentialOAuthController {
	logger := zerolog.Nop()
	c := &ClientCredentialOAuthController{
		configURL: configURL,
		logger:    logger,
	}

	for _, opt := range opts {
		opt(c)
	}

	c.logger = c.logger.With().Str("component", "oauth-controller").Logger()

	return c
}

func (c *ClientCredentialOAuthController) init(ctx context.Context) error {
	c.initOnce.Do(func() {
		resp, err := http.Get(c.configURL)
		if err != nil {
			c.initErr = fmt.Errorf("failed to fetch OAuth config: %w", err)
			return
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			c.initErr = fmt.Errorf("failed to read OAuth config response: %w", err)
			return
		}

		var config struct {
			TokenEndpoint string `json:"token_endpoint"`
		}
		if err := json.Unmarshal(body, &config); err != nil {
			c.initErr = fmt.Errorf("failed to parse OAuth config: %w", err)
			return
		}

		c.tokenEndpoint = config.TokenEndpoint
	})

	return c.initErr
}

func (c *ClientCredentialOAuthController) UserID() string {
	return c.userID
}

func (c *ClientCredentialOAuthController) GetUserToken(ctx context.Context) (*model.AuthContext, error) {
	if err := c.init(ctx); err != nil {
		return nil, err
	}

	if c.userID == "" {
		return nil, fmt.Errorf("userID not set")
	}
	if c.userSecret == "" {
		return nil, fmt.Errorf("userSecret not set")
	}

	if token, ok := c.accessTokens.Load("user"); ok {
		if tokenStr, ok := token.(string); ok && c.isJWTValid(tokenStr) {
			c.logger.Debug().Msg("Using cached user token")
			return &model.AuthContext{
				UserID:      c.userID,
				AccessToken: tokenStr,
			}, nil
		}
	}

	if pendingPromise, loaded := c.pendingReqs.LoadOrStore("user", make(chan string, 1)); loaded {
		c.logger.Debug().Msg("Waiting for pending user token request")
		select {
		case token := <-pendingPromise.(chan string):
			return &model.AuthContext{
				UserID:      c.userID,
				AccessToken: token,
			}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	c.logger.Debug().Msg("Creating new user token")

	token, err := c.fetchToken(ctx, c.userID, c.userSecret)
	if err != nil {
		c.pendingReqs.Delete("user")
		return nil, err
	}

	c.accessTokens.Store("user", token)

	if ch, ok := c.pendingReqs.Load("user"); ok {
		ch.(chan string) <- token
		c.pendingReqs.Delete("user")
	}

	return &model.AuthContext{
		UserID:      c.userID,
		AccessToken: token,
	}, nil
}

func (c *ClientCredentialOAuthController) GetAdminToken(ctx context.Context) (*model.AuthContext, error) {
	if err := c.init(ctx); err != nil {
		return nil, err
	}

	if c.adminID == "" {
		return nil, fmt.Errorf("adminID not set")
	}
	if c.adminSecret == "" {
		return nil, fmt.Errorf("adminSecret not set")
	}

	if token, ok := c.accessTokens.Load("admin"); ok {
		if tokenStr, ok := token.(string); ok && c.isJWTValid(tokenStr) {
			c.logger.Debug().Msg("Using cached admin token")
			return &model.AuthContext{
				UserID:      c.adminID,
				AccessToken: tokenStr,
			}, nil
		}
	}

	if pendingPromise, loaded := c.pendingReqs.LoadOrStore("admin", make(chan string, 1)); loaded {
		c.logger.Debug().Msg("Waiting for pending admin token request")
		select {
		case token := <-pendingPromise.(chan string):
			return &model.AuthContext{
				UserID:      c.adminID,
				AccessToken: token,
			}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	c.logger.Debug().Msg("Creating new admin token")

	token, err := c.fetchToken(ctx, c.adminID, c.adminSecret)
	if err != nil {
		c.pendingReqs.Delete("admin")
		return nil, err
	}

	c.accessTokens.Store("admin", token)

	if ch, ok := c.pendingReqs.Load("admin"); ok {
		ch.(chan string) <- token
		c.pendingReqs.Delete("admin")
	}

	return &model.AuthContext{
		UserID:      c.adminID,
		AccessToken: token,
	}, nil
}

func (c *ClientCredentialOAuthController) fetchToken(ctx context.Context, clientID, clientSecret string) (string, error) {
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	if c.scope != "" {
		data.Set("scope", c.scope)
	}
	if c.audience != "" {
		data.Set("audience", c.audience)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		AccessToken string `json:"access_token"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	return result.AccessToken, nil
}

func (c *ClientCredentialOAuthController) isJWTValid(tokenStr string) bool {
	token, _, err := jwt.NewParser().ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil {
		return false
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return false
	}

	exp, ok := claims["exp"].(float64)
	if !ok {
		return false
	}

	return time.Now().Unix() < int64(exp)
}
