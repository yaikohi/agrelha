package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"agrelha/internal/ports"
)

const (
	DefaultSocketPath = "/var/run/docker.sock"
	DefaultAPIVersion = "v1.43"
)

// ContainerState holds runtime state information from Docker inspect.
type ContainerState struct {
	Status     string `json:"Status"` // "running", "exited", "created", etc.
	Running    bool   `json:"Running"`
	StartedAt  string `json:"StartedAt"`
	FinishedAt string `json:"FinishedAt"`
	ExitCode   int    `json:"ExitCode"`
	Health     *struct {
		Status string `json:"Status"` // "healthy", "unhealthy", "starting"
	} `json:"Health,omitempty"`
}

// ContainerInspect represents the inspect JSON response.
type ContainerInspect struct {
	ID    string         `json:"Id"`
	Name  string         `json:"Name"`
	State ContainerState `json:"State"`
}

// ContainerStats represents the stats JSON response.
type ContainerStats struct {
	MemoryStats struct {
		Usage int64 `json:"usage"`
		Limit int64 `json:"limit"`
	} `json:"memory_stats"`
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     uint32 `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
}

// Client defines the interface for interacting with the Docker Engine.
type Client interface {
	ContainerStart(ctx context.Context, id string) error
	ContainerStop(ctx context.Context, id string) error
	ContainerRestart(ctx context.Context, id string) error
	ContainerInspect(ctx context.Context, id string) (ContainerInspect, error)
	ContainerStats(ctx context.Context, id string) (ContainerStats, error)
	ContainerLogs(ctx context.Context, id string, opts ports.LogOptions) (io.ReadCloser, error)
}

// SocketClient communicates with Docker Engine over Unix socket or HTTP.
type SocketClient struct {
	http       *http.Client
	baseURL    string
	apiVersion string
}

// SocketClientOption configures SocketClient.
type SocketClientOption func(*SocketClient)

func WithHTTPClient(c *http.Client) SocketClientOption {
	return func(sc *SocketClient) {
		sc.http = c
	}
}

func WithBaseURL(u string) SocketClientOption {
	return func(sc *SocketClient) {
		sc.baseURL = u
	}
}

// NewSocketClient creates a client talking to the Docker daemon over unix socket.
func NewSocketClient(socketPath string, opts ...SocketClientOption) *SocketClient {
	if socketPath == "" {
		if host := os.Getenv("DOCKER_HOST"); host != "" {
			if after, ok := strings.CutPrefix(host, "unix://"); ok {
				socketPath = after
			}
		}
	}
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}

	sc := &SocketClient{
		http:       &http.Client{Transport: transport, Timeout: 30 * time.Second},
		baseURL:    "http://localhost",
		apiVersion: DefaultAPIVersion,
	}
	for _, opt := range opts {
		opt(sc)
	}
	return sc
}

func (c *SocketClient) url(path string) string {
	return fmt.Sprintf("%s/%s%s", c.baseURL, c.apiVersion, path)
}

func (c *SocketClient) ContainerStart(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(fmt.Sprintf("/containers/%s/start", id)), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("start container %s: status %d: %s", id, resp.StatusCode, strings.TrimSpace(string(body)))
}

func (c *SocketClient) ContainerStop(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(fmt.Sprintf("/containers/%s/stop?t=10", id)), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("stop container %s: status %d: %s", id, resp.StatusCode, strings.TrimSpace(string(body)))
}

func (c *SocketClient) ContainerRestart(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(fmt.Sprintf("/containers/%s/restart?t=10", id)), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("restart container %s: status %d: %s", id, resp.StatusCode, strings.TrimSpace(string(body)))
}

func (c *SocketClient) ContainerInspect(ctx context.Context, id string) (ContainerInspect, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(fmt.Sprintf("/containers/%s/json", id)), nil)
	if err != nil {
		return ContainerInspect{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return ContainerInspect{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ContainerInspect{}, os.ErrNotExist
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return ContainerInspect{}, fmt.Errorf("inspect container %s: status %d: %s", id, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var ci ContainerInspect
	if err := json.NewDecoder(resp.Body).Decode(&ci); err != nil {
		return ContainerInspect{}, fmt.Errorf("decode inspect response: %w", err)
	}
	return ci, nil
}

func (c *SocketClient) ContainerStats(ctx context.Context, id string) (ContainerStats, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(fmt.Sprintf("/containers/%s/stats?stream=false", id)), nil)
	if err != nil {
		return ContainerStats{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return ContainerStats{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return ContainerStats{}, fmt.Errorf("stats container %s: status %d: %s", id, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var cs ContainerStats
	if err := json.NewDecoder(resp.Body).Decode(&cs); err != nil {
		return ContainerStats{}, fmt.Errorf("decode stats response: %w", err)
	}
	return cs, nil
}

func (c *SocketClient) ContainerLogs(ctx context.Context, id string, opts ports.LogOptions) (io.ReadCloser, error) {
	tail := opts.Tail
	if tail <= 0 {
		tail = 200
	}
	follow := "0"
	if opts.Follow {
		follow = "1"
	}
	path := fmt.Sprintf("/containers/%s/logs?stdout=1&stderr=1&tail=%d&follow=%s", id, tail, follow)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(path), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("logs container %s: status %d: %s", id, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return newDemuxLogReader(resp.Body), nil
}

// demuxLogReader strips Docker stream framing headers (8 bytes per frame).
type demuxLogReader struct {
	reader io.ReadCloser
	buf    bytes.Buffer
}

func newDemuxLogReader(rc io.ReadCloser) io.ReadCloser {
	return &demuxLogReader{reader: rc}
}

func (r *demuxLogReader) Close() error {
	return r.reader.Close()
}

func (r *demuxLogReader) Read(p []byte) (int, error) {
	if r.buf.Len() > 0 {
		return r.buf.Read(p)
	}

	var header [8]byte
	_, err := io.ReadFull(r.reader, header[:])
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, io.EOF
		}
		return 0, err
	}

	// Check if this looks like a docker multiplex header (STREAM_TYPE=1|2 and 3 zeroes)
	if (header[0] == 1 || header[0] == 2) && header[1] == 0 && header[2] == 0 && header[3] == 0 {
		frameLen := binary.BigEndian.Uint32(header[4:8])
		if frameLen > 0 {
			lr := io.LimitReader(r.reader, int64(frameLen))
			if _, err := io.Copy(&r.buf, lr); err != nil {
				return 0, err
			}
		}
		return r.buf.Read(p)
	}

	// Fallback for non-multiplexed / TTY output: include the 8 header bytes as raw data
	r.buf.Write(header[:])
	return r.buf.Read(p)
}
