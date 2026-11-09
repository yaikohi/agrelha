package zitadel

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

const DefaultTimeout = 20 * time.Second

type Config struct {
	Issuer     string
	APIBaseURL string
	TokenURL   string
	ProjectID  string
	ServiceKey []byte
	HTTPClient *http.Client
}

type serviceKey struct {
	Type   string `json:"type"`
	KeyID  string `json:"keyId"`
	Key    string `json:"key"`
	UserID string `json:"userId"`
}

type Client struct {
	base      string
	tokenURL  string
	issuer    string
	projectID string
	key       serviceKey
	http      *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

var _ ports.RoleRegistry = (*Client)(nil)

func New(cfg Config) (*Client, error) {
	if cfg.ProjectID == "" {
		return nil, errors.New("zitadel: empty project id")
	}
	if len(cfg.ServiceKey) == 0 {
		return nil, errors.New("zitadel: empty service key")
	}
	var k serviceKey
	if err := json.Unmarshal(cfg.ServiceKey, &k); err != nil {
		return nil, fmt.Errorf("zitadel: parse service key: %w", err)
	}
	if k.KeyID == "" || k.Key == "" || k.UserID == "" {
		return nil, errors.New("zitadel: service key missing keyId, key or userId")
	}
	issuer := strings.TrimSuffix(cfg.Issuer, "/")
	base := strings.TrimSuffix(cfg.APIBaseURL, "/")
	if base == "" {
		base = issuer
	}
	tokenURL := cfg.TokenURL
	if tokenURL == "" {
		tokenURL = issuer + "/oauth/v2/token"
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: DefaultTimeout}
	}
	return &Client{base: base, tokenURL: tokenURL, issuer: issuer, projectID: cfg.ProjectID, key: k, http: hc}, nil
}

func parseRSAPrivateKey(pemData string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		if !strings.Contains(pemData, "\n") {
			return nil, errors.New("zitadel: service key is not valid PEM: it contains no newlines, " +
				"which usually means the JSON was passed through a systemd EnvironmentFile - that parser " +
				"consumes the backslash of every \\n escape. Pass ZITADEL_SERVICE_KEY_FILE with a path instead")
		}
		return nil, errors.New("zitadel: service key is not valid PEM")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("zitadel: parse private key: %w", err)
	}
	k, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("zitadel: service key is not an RSA key")
	}
	return k, nil
}

func (c *Client) assertion(now time.Time) (string, error) {
	priv, err := parseRSAPrivateKey(c.key.Key)
	if err != nil {
		return "", err
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: priv},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", c.key.KeyID),
	)
	if err != nil {
		return "", fmt.Errorf("new signer: %w", err)
	}
	claims := jwt.Claims{
		Issuer:   c.key.UserID,
		Subject:  c.key.UserID,
		Audience: jwt.Audience{c.issuer},
		IssuedAt: jwt.NewNumericDate(now),
		Expiry:   jwt.NewNumericDate(now.Add(time.Hour)),
	}
	return jwt.Signed(signer).Claims(claims).Serialize()
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.token != "" && now.Before(c.expires.Add(-60*time.Second)) {
		return c.token, nil
	}
	a, err := c.assertion(now)
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", a)
	form.Set("scope", "openid urn:zitadel:iam:org:project:id:zitadel:aud")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("zitadel token: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("zitadel token: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("zitadel token: decode: %w", err)
	}
	if out.AccessToken == "" {
		return "", errors.New("zitadel token: empty access_token")
	}
	ttl := out.ExpiresIn
	if ttl <= 0 {
		ttl = 3600
	}
	c.token = out.AccessToken
	c.expires = now.Add(time.Duration(ttl) * time.Second)
	return c.token, nil
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) (int, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(b)
	}
	tok, err := c.accessToken(ctx)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("zitadel %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("zitadel %s %s: http %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("zitadel %s %s: decode: %w", method, path, err)
		}
	}
	return resp.StatusCode, nil
}

func alreadyExists(status int, err error) bool {
	if status == http.StatusConflict {
		return true
	}
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "already exists")
}

func notFound(status int, err error) bool {
	if status == http.StatusNotFound {
		return true
	}
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "not found")
}

const (
	projectSvc = "/zitadel.project.v2.ProjectService/"
	authSvc    = "/zitadel.authorization.v2.AuthorizationService/"
)

func (c *Client) AddRole(ctx context.Context, role domain.Role, displayName string) error {
	in := map[string]any{
		"projectId":   c.projectID,
		"roleKey":     string(role),
		"displayName": displayName,
		"group":       "instances",
	}
	status, err := c.do(ctx, http.MethodPost, projectSvc+"AddProjectRole", in, nil)
	if err != nil {
		if alreadyExists(status, err) {
			return ports.ErrRoleExists
		}
		return err
	}
	return nil
}

func (c *Client) RemoveRole(ctx context.Context, role domain.Role) error {
	in := map[string]any{"projectId": c.projectID, "roleKey": string(role)}
	status, err := c.do(ctx, http.MethodPost, projectSvc+"RemoveProjectRole", in, nil)
	if err != nil {
		if notFound(status, err) {
			return nil
		}
		return err
	}
	return nil
}

type authorization struct {
	ID    string `json:"id"`
	Roles []struct {
		Key string `json:"key"`
	} `json:"roles"`
}

func (a authorization) keys() []string {
	out := make([]string, 0, len(a.Roles))
	for _, r := range a.Roles {
		out = append(out, r.Key)
	}
	return out
}

// authorizationsFor returns every authorization the subject holds on this
// project. Zitadel can hold more than one - a user granted through two
// organizations has two - and reading only the first hid the roles in the rest:
// a Grant looked applied and the account page listed nothing.
func (c *Client) authorizationsFor(ctx context.Context, subject string) ([]authorization, error) {
	in := map[string]any{
		"pagination": map[string]any{"offset": "0", "limit": 100, "asc": true},
		"filters": []any{
			map[string]any{"inUserIds": map[string]any{"ids": []string{subject}}},
			map[string]any{"projectId": map[string]any{"id": c.projectID}},
		},
	}
	var out struct {
		Authorizations []authorization `json:"authorizations"`
	}
	if _, err := c.do(ctx, http.MethodPost, authSvc+"ListAuthorizations", in, &out); err != nil {
		return nil, err
	}
	return out.Authorizations, nil
}

func (c *Client) RolesFor(ctx context.Context, subject string) ([]domain.Role, error) {
	auths, err := c.authorizationsFor(ctx, subject)
	if err != nil {
		return nil, err
	}
	var out []domain.Role
	seen := map[string]bool{}
	for _, a := range auths {
		for _, k := range a.keys() {
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, domain.Role(k))
		}
	}
	return out, nil
}

func (c *Client) GrantRole(ctx context.Context, subject string, role domain.Role) error {
	auths, err := c.authorizationsFor(ctx, subject)
	if err != nil {
		return err
	}
	if len(auths) == 0 {
		in := map[string]any{
			"userId":    subject,
			"projectId": c.projectID,
			"roleKeys":  []string{string(role)},
		}
		_, err := c.do(ctx, http.MethodPost, authSvc+"CreateAuthorization", in, nil)
		return err
	}
	for _, a := range auths {
		if slices.Contains(a.keys(), string(role)) {
			return nil
		}
	}
	first := auths[0]
	in := map[string]any{"id": first.ID, "roleKeys": append(first.keys(), string(role))}
	_, err = c.do(ctx, http.MethodPost, authSvc+"UpdateAuthorization", in, nil)
	return err
}

// RevokeRole takes the role off every authorization that carries it. Leaving it
// on a second authorization would revoke nothing: the next role lookup still
// reports it, and the account keeps the world it was just denied.
func (c *Client) RevokeRole(ctx context.Context, subject string, role domain.Role) error {
	auths, err := c.authorizationsFor(ctx, subject)
	if err != nil {
		return err
	}
	for _, a := range auths {
		if !slices.Contains(a.keys(), string(role)) {
			continue
		}
		var keys []string
		for _, k := range a.keys() {
			if k != string(role) {
				keys = append(keys, k)
			}
		}
		if len(keys) == 0 {
			status, delErr := c.do(ctx, http.MethodPost, authSvc+"DeleteAuthorization", map[string]any{"id": a.ID}, nil)
			if delErr != nil && !notFound(status, delErr) {
				return delErr
			}
			continue
		}
		if _, updErr := c.do(ctx, http.MethodPost, authSvc+"UpdateAuthorization", map[string]any{"id": a.ID, "roleKeys": keys}, nil); updErr != nil {
			return updErr
		}
	}
	return nil
}
