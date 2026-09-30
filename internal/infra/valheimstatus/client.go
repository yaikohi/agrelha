package valheimstatus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Status struct {
	ServerName  string `json:"server_name"`
	PlayerCount int    `json:"player_count"`
	Error       any    `json:"error"`
}

type Client struct {
	http  *http.Client
	urlFn func(service string) string
}

type Option func(*Client)

func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

func WithURL(fn func(service string) string) Option { return func(c *Client) { c.urlFn = fn } }

func New(namespace string, opts ...Option) *Client {
	c := &Client{
		http: &http.Client{Timeout: 3 * time.Second},
		urlFn: func(service string) string {
			return fmt.Sprintf("http://%s.%s.svc.cluster.local:9001/status.json", service, namespace)
		},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *Client) Players(ctx context.Context, service string) (int, bool) {
	st, err := c.Status(ctx, service)
	if err != nil || st == nil {
		return 0, false
	}

	if st.Error != nil {
		if s, ok := st.Error.(string); !ok || s != "" {
			return 0, false
		}
	}
	return st.PlayerCount, true
}

func (c *Client) Status(ctx context.Context, service string) (*Status, error) {
	if c == nil || service == "" {
		return nil, fmt.Errorf("no service to query")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.urlFn(service), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status endpoint returned %s", resp.Status)
	}
	var st Status
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return nil, err
	}
	return &st, nil
}
