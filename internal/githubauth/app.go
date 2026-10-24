package githubauth

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// jwtExpiry is the lifetime of the JWT used to authenticate as the App.
	jwtExpiry = 10 * time.Minute

	// tokenRequestTimeout is the HTTP timeout for the token exchange.
	tokenRequestTimeout = 10 * time.Second

	// defaultAPIBaseURL targets github.com.
	defaultAPIBaseURL = "https://api.github.com"
)

// AppProvider mints short-lived installation access tokens for a GitHub
// App. Each call to GenerateToken signs a fresh JWT and exchanges it for
// an installation token via the GitHub REST API.
type AppProvider struct {
	appID          int64
	installationID int64
	privateKey     *rsa.PrivateKey
	apiBaseURL     string
	httpClient     *http.Client
}

// Compile-time check.
var _ TokenGenerator = (*AppProvider)(nil)

// WithAPIBaseURL overrides the GitHub API base URL. Trailing slashes are
// trimmed. An empty string is a no-op.
func WithAPIBaseURL(u string) Option {
	return func(p *AppProvider) {
		if u == "" {
			return
		}

		p.apiBaseURL = strings.TrimRight(u, "/")
	}
}

// NewAppProvider constructs an AppProvider by reading the PEM private
// key from disk.
func NewAppProvider(appID, installationID int64, privateKeyPath string, opts ...Option) (*AppProvider, error) {
	keyData, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}

	key, err := jwt.ParseRSAPrivateKeyFromPEM(keyData)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	p := &AppProvider{
		appID:          appID,
		installationID: installationID,
		privateKey:     key,
		apiBaseURL:     defaultAPIBaseURL,
		httpClient:     &http.Client{Timeout: tokenRequestTimeout},
	}
	for _, opt := range opts {
		opt(p)
	}

	return p, nil
}

// NewAppProviderWithKey constructs an AppProvider from an already-parsed
// RSA key and a custom API base URL, as used by the credential pool.
func NewAppProviderWithKey(appID, installationID int64, key *rsa.PrivateKey, apiBaseURL string) (*AppProvider, error) {
	if key == nil {
		return nil, errors.New("private key is nil")
	}

	base := apiBaseURL
	if base == "" {
		base = defaultAPIBaseURL
	}

	return &AppProvider{
		appID:          appID,
		installationID: installationID,
		privateKey:     key,
		apiBaseURL:     strings.TrimRight(base, "/"),
		httpClient:     &http.Client{Timeout: tokenRequestTimeout},
	}, nil
}

// createJWT builds a signed JWT for authenticating as the GitHub App.
func (p *AppProvider) createJWT() (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    strconv.FormatInt(p.appID, 10),
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)), // skew tolerance
		ExpiresAt: jwt.NewNumericDate(now.Add(jwtExpiry)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)

	signed, err := token.SignedString(p.privateKey)
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}

	return signed, nil
}

// installationToken matches the relevant fields of GitHub's response.
type installationToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// GenerateToken signs a JWT, exchanges it via GitHub's REST API, and
// returns the resulting installation access token plus its expiry.
func (p *AppProvider) GenerateToken(ctx context.Context) (string, time.Time, error) {
	return p.mint(ctx, nil)
}

// GenerateRepoToken mints an installation token limited to the named
// repository. An empty name is refused before any request so the call can
// never yield an installation-wide token.
func (p *AppProvider) GenerateRepoToken(ctx context.Context, repo string) (string, time.Time, error) {
	if repo == "" {
		return "", time.Time{}, errors.New("repo is required")
	}

	body, err := json.Marshal(map[string][]string{"repositories": {repo}})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("marshal request: %w", err)
	}

	return p.mint(ctx, body)
}

// mint exchanges a fresh JWT for an installation token; a nil body requests
// the installation's full access.
func (p *AppProvider) mint(ctx context.Context, reqBody []byte) (string, time.Time, error) {
	jwtToken, err := p.createJWT()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create JWT: %w", err)
	}

	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", p.apiBaseURL, p.installationID)

	var rdr io.Reader
	if reqBody != nil {
		rdr = bytes.NewReader(reqBody)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, rdr)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create request: %w", err)
	}

	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("request token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return "", time.Time{}, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var t installationToken
	if err := json.Unmarshal(body, &t); err != nil {
		return "", time.Time{}, fmt.Errorf("parse token response: %w", err)
	}

	if t.Token == "" {
		return "", time.Time{}, fmt.Errorf("empty token in response")
	}

	return t.Token, t.ExpiresAt, nil
}
