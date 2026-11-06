package rcon

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	packetTypeAuth         int32 = 3
	packetTypeAuthResponse int32 = 2
	packetTypeExecCommand  int32 = 2
	packetTypeResponse     int32 = 0
)

var ErrNotConfigured = errors.New("rcon: no password configured")

// Tracer observes one RCON command. It returns a derived context and a finish
// func that receives the command's error, which callers routinely discard.
type Tracer func(ctx context.Context, name string) (context.Context, func(error))

type Option func(*Client)

func WithTracer(t Tracer) Option {
	return func(c *Client) {
		if t != nil {
			c.trace = t
		}
	}
}

func noTrace(ctx context.Context, _ string) (context.Context, func(error)) {
	return ctx, func(error) {}
}

type Client struct {
	addr     string
	password string
	timeout  time.Duration
	trace    Tracer

	mu    sync.Mutex
	conn  net.Conn
	reqID int32
}

func NewClient(addr, password string, timeout time.Duration, opts ...Option) *Client {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	c := &Client{
		addr:     addr,
		password: password,
		timeout:  timeout,
		reqID:    1,
		trace:    noTrace,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *Client) connect() error {
	if c.conn != nil {
		return nil
	}
	conn, err := net.DialTimeout("tcp", c.addr, c.timeout)
	if err != nil {
		return fmt.Errorf("rcon dial: %w", err)
	}
	c.conn = conn

	// Authenticate
	c.reqID++
	if err := c.writePacket(c.reqID, packetTypeAuth, c.password); err != nil {
		c.conn.Close()
		c.conn = nil
		return fmt.Errorf("rcon send auth: %w", err)
	}

	resID, resType, _, err := c.readPacket()
	if err != nil {
		c.conn.Close()
		c.conn = nil
		return fmt.Errorf("rcon read auth response: %w", err)
	}

	if resID == -1 || (resType != packetTypeAuthResponse && resType != packetTypeResponse) {
		c.conn.Close()
		c.conn = nil
		return errors.New("rcon: authentication failed (bad password)")
	}

	return nil
}

func (c *Client) writePacket(id, typ int32, body string) error {
	payload := []byte(body)
	length := int32(4 + 4 + len(payload) + 2) // ID (4) + Type (4) + body + 2 null bytes

	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, length)
	_ = binary.Write(buf, binary.LittleEndian, id)
	_ = binary.Write(buf, binary.LittleEndian, typ)
	buf.Write(payload)
	buf.WriteByte(0)
	buf.WriteByte(0)

	_ = c.conn.SetDeadline(time.Now().Add(c.timeout))
	_, err := c.conn.Write(buf.Bytes())
	return err
}

func (c *Client) readPacket() (int32, int32, string, error) {
	_ = c.conn.SetDeadline(time.Now().Add(c.timeout))

	var length int32
	if err := binary.Read(c.conn, binary.LittleEndian, &length); err != nil {
		return 0, 0, "", err
	}
	if length < 10 || length > 4096 {
		return 0, 0, "", fmt.Errorf("invalid rcon packet length: %d", length)
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(c.conn, data); err != nil {
		return 0, 0, "", err
	}

	buf := bytes.NewReader(data)
	var id, typ int32
	_ = binary.Read(buf, binary.LittleEndian, &id)
	_ = binary.Read(buf, binary.LittleEndian, &typ)

	bodyBytes := data[8 : len(data)-2] // exclude ID, type, and 2 null bytes
	return id, typ, strings.TrimRight(string(bodyBytes), "\x00"), nil
}

// Execute sends an RCON command to the server and returns the console response.
// Callers routinely discard the error, so failures are recorded on the span.
func (c *Client) Execute(ctx context.Context, cmd string) (string, error) {
	if c == nil {
		return "", ErrNotConfigured
	}

	_, end := c.trace(ctx, "rcon.execute "+commandVerb(cmd))
	var failed error
	defer func() { end(failed) }()

	fail := func(err error) (string, error) {
		failed = err
		return "", err
	}

	if err := ctx.Err(); err != nil {
		return fail(err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.connect(); err != nil {
		return fail(err)
	}

	c.reqID++
	if err := c.writePacket(c.reqID, packetTypeExecCommand, cmd); err != nil {
		c.conn.Close()
		c.conn = nil
		return fail(fmt.Errorf("rcon send cmd: %w", err))
	}

	_, _, response, err := c.readPacket()
	if err != nil {
		c.conn.Close()
		c.conn = nil
		return fail(fmt.Errorf("rcon read response: %w", err))
	}

	return response, nil
}

func commandVerb(cmd string) string {
	verb, _, _ := strings.Cut(strings.TrimSpace(cmd), " ")
	return verb
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

type Pool struct {
	password string
	timeout  time.Duration
	opts     []Option
	mu       sync.Mutex
	clients  map[string]*Client
}

func NewPool(password string, timeout time.Duration, opts ...Option) *Pool {
	return &Pool{
		password: password,
		timeout:  timeout,
		opts:     opts,
		clients:  make(map[string]*Client),
	}
}

func (p *Pool) ClientFor(addr string) *Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.clients[addr]; ok {
		return c
	}
	c := NewClient(addr, p.password, p.timeout, p.opts...)
	p.clients[addr] = c
	return c
}
