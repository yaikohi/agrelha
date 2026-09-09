package minecraft

import (
	"bytes"
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

type RconClient struct {
	addr     string
	password string
	timeout  time.Duration

	mu    sync.Mutex
	conn  net.Conn
	reqID int32
}

func NewRconClient(addr, password string, timeout time.Duration) *RconClient {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &RconClient{
		addr:     addr,
		password: password,
		timeout:  timeout,
		reqID:    1,
	}
}

func (c *RconClient) connect() error {
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

func (c *RconClient) writePacket(id, typ int32, body string) error {
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

func (c *RconClient) readPacket() (int32, int32, string, error) {
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
func (c *RconClient) Execute(cmd string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.connect(); err != nil {
		return "", err
	}

	c.reqID++
	if err := c.writePacket(c.reqID, packetTypeExecCommand, cmd); err != nil {
		c.conn.Close()
		c.conn = nil
		return "", fmt.Errorf("rcon send cmd: %w", err)
	}

	_, _, response, err := c.readPacket()
	if err != nil {
		c.conn.Close()
		c.conn = nil
		return "", fmt.Errorf("rcon read response: %w", err)
	}

	return response, nil
}

func (c *RconClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

type RconPool struct {
	password string
	timeout  time.Duration
	mu       sync.Mutex
	clients  map[string]*RconClient
}

func NewRconPool(password string, timeout time.Duration) *RconPool {
	return &RconPool{
		password: password,
		timeout:  timeout,
		clients:  make(map[string]*RconClient),
	}
}

func (p *RconPool) ClientFor(addr string) *RconClient {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.clients[addr]; ok {
		return c
	}
	c := NewRconClient(addr, p.password, p.timeout)
	p.clients[addr] = c
	return c
}
