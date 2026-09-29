// Package valheimstatus reads the lloesche image's STATUS_HTTP endpoint.
//
// It is how agrelha learns whether anyone is playing. The alternative, tailing
// the server log, was wired to a single deployment name that no longer exists,
// so it reported nothing at all; this asks the server directly, per world.
package valheimstatus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Status is the part of status.json agrelha acts on.
type Status struct {
	ServerName  string `json:"server_name"`
	PlayerCount int    `json:"player_count"`
	Error       any    `json:"error"`
}

// Client polls one Valheim instance's status endpoint.
type Client struct {
	http  *http.Client
	urlFn func(service string) string
}

type Option func(*Client)

func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithURL replaces how a service name becomes a status URL. The default is the
// in-cluster service address; this is the seam for pointing somewhere else.
func WithURL(fn func(service string) string) Option { return func(c *Client) { c.urlFn = fn } }

// New builds a client for services in the given namespace.
func New(namespace string, opts ...Option) *Client {
	c := &Client{
		// Short: this is asked on a page load and before a restart, and a world
		// that is down should answer "unknown" quickly rather than hang the
		// caller waiting for a server that will never reply.
		http: &http.Client{Timeout: 3 * time.Second},
		urlFn: func(service string) string {
			// Port 9001 is what the Deployment and Service declare. The image
			// serves on :80 unless STATUS_HTTP_PORT says otherwise, so a world
			// missing that env var answers "unknown" rather than wrongly "empty".
			return fmt.Sprintf("http://%s.%s.svc.cluster.local:9001/status.json", service, namespace)
		},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Players reports how many people are connected to a world.
//
// The second return is whether the answer is known at all. A server that is
// stopped, starting, or unreachable is not the same as an empty one, and the
// difference decides whether a restart may proceed unattended.
func (c *Client) Players(ctx context.Context, service string) (int, bool) {
	st, err := c.Status(ctx, service)
	if err != nil || st == nil {
		return 0, false
	}
	// The image reports its own failures in this field - an A2S query that did
	// not answer leaves player_count at zero, which must not read as empty.
	if st.Error != nil {
		if s, ok := st.Error.(string); !ok || s != "" {
			return 0, false
		}
	}
	return st.PlayerCount, true
}

// Status fetches and parses status.json for one service.
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
