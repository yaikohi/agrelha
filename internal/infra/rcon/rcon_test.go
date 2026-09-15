package rcon

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// mockRconServer starts a test TCP listener speaking Source RCON protocol.
func mockRconServer(t *testing.T, expectedPassword string) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}

	stop := make(chan struct{})

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-stop:
					return
				default:
					return
				}
			}
			go handleConn(conn, expectedPassword)
		}
	}()

	return ln.Addr().String(), func() {
		close(stop)
		_ = ln.Close()
	}
}

func handleConn(conn net.Conn, expectedPassword string) {
	defer conn.Close()

	for {
		var length int32
		if err := binary.Read(conn, binary.LittleEndian, &length); err != nil {
			return
		}
		if length < 10 || length > 4096 {
			return
		}
		data := make([]byte, length)
		if _, err := io.ReadFull(conn, data); err != nil {
			return
		}

		buf := bytes.NewReader(data)
		var id, typ int32
		_ = binary.Read(buf, binary.LittleEndian, &id)
		_ = binary.Read(buf, binary.LittleEndian, &typ)
		body := string(data[8 : len(data)-2])

		switch typ {
		case packetTypeAuth:
			if body == expectedPassword {
				// Success auth response
				sendPacket(conn, id, packetTypeAuthResponse, "")
			} else {
				// Failed auth response has ID = -1
				sendPacket(conn, -1, packetTypeAuthResponse, "")
			}
		case packetTypeExecCommand:
			if strings.HasPrefix(body, "crash") {
				// Intentionally close connection to simulate server crash
				return
			}
			sendPacket(conn, id, packetTypeResponse, "executed: "+body)
		}
	}
}

func sendPacket(conn net.Conn, id, typ int32, body string) {
	payload := []byte(body)
	length := int32(4 + 4 + len(payload) + 2)
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, length)
	_ = binary.Write(buf, binary.LittleEndian, id)
	_ = binary.Write(buf, binary.LittleEndian, typ)
	buf.Write(payload)
	buf.WriteByte(0)
	buf.WriteByte(0)
	_, _ = conn.Write(buf.Bytes())
}

func TestRcon_Execute_SuccessAndReuse(t *testing.T) {
	addr, cleanup := mockRconServer(t, "topsecret")
	defer cleanup()

	client := NewClient(addr, "topsecret", 0) // timeout <= 0 defaults to 5s
	defer client.Close()

	// 1. First execution (connects + auth + command)
	resp, err := client.Execute("whitelist list")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if resp != "executed: whitelist list" {
		t.Errorf("got %q, want 'executed: whitelist list'", resp)
	}

	// 2. Second execution (reuses existing connection)
	resp2, err := client.Execute("time set day")
	if err != nil {
		t.Fatalf("Execute 2 failed: %v", err)
	}
	if resp2 != "executed: time set day" {
		t.Errorf("got %q, want 'executed: time set day'", resp2)
	}
}

func TestRcon_Execute_AuthFailure(t *testing.T) {
	addr, cleanup := mockRconServer(t, "topsecret")
	defer cleanup()

	client := NewClient(addr, "wrongpassword", 500*time.Millisecond)
	defer client.Close()

	_, err := client.Execute("whitelist list")
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("expected authentication failed error, got: %v", err)
	}
}

func TestRcon_Execute_NilClient(t *testing.T) {
	var c *Client
	_, err := c.Execute("help")
	if err != ErrNotConfigured {
		t.Errorf("expected ErrNotConfigured, got %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close on nil client should be nil, got %v", err)
	}
}

func TestRcon_Execute_ConnectionDrop(t *testing.T) {
	addr, cleanup := mockRconServer(t, "topsecret")
	defer cleanup()

	client := NewClient(addr, "topsecret", 500*time.Millisecond)
	defer client.Close()

	// Trigger simulated server drop
	_, err := client.Execute("crash")
	if err == nil {
		t.Fatal("expected error on server crash")
	}

	// Next command should reconnect and succeed
	resp, err := client.Execute("say recovered")
	if err != nil {
		t.Fatalf("reconnect Execute failed: %v", err)
	}
	if resp != "executed: say recovered" {
		t.Errorf("unexpected response: %s", resp)
	}
}

func TestRcon_DialFailure(t *testing.T) {
	// Connect to non-listening address
	client := NewClient("127.0.0.1:54321", "pass", 50*time.Millisecond)
	_, err := client.Execute("test")
	if err == nil {
		t.Fatal("expected dial error, got nil")
	}
}

func TestRcon_InvalidPacketLength(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Read auth request from client
		var l int32
		_ = binary.Read(conn, binary.LittleEndian, &l)
		d := make([]byte, l)
		_, _ = io.ReadFull(conn, d)

		// Send invalid length < 10
		_ = binary.Write(conn, binary.LittleEndian, int32(5))
	}()

	client := NewClient(ln.Addr().String(), "pass", 500*time.Millisecond)
	defer client.Close()

	_, err = client.Execute("test")
	if err == nil || !strings.Contains(err.Error(), "invalid rcon packet length") {
		t.Fatalf("expected invalid rcon packet length error, got %v", err)
	}
}

func TestRcon_Pool(t *testing.T) {
	pool := NewPool("mypass", time.Second)
	c1 := pool.ClientFor("127.0.0.1:25575")
	if c1 == nil {
		t.Fatal("expected non-nil client")
	}
	c2 := pool.ClientFor("127.0.0.1:25575")
	if c1 != c2 {
		t.Error("ClientFor should return cached client instance for same address")
	}
	c3 := pool.ClientFor("127.0.0.1:25576")
	if c3 == c1 {
		t.Error("ClientFor should create different client for different address")
	}
}

func TestRcon_WriteAndReadFailures(t *testing.T) {
	// 1. Connection closed immediately by server -> write auth fails or read auth fails
	t.Run("server closes immediately on connect", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()

		go func() {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Close connection immediately
			_ = conn.Close()
		}()

		client := NewClient(ln.Addr().String(), "pass", 100*time.Millisecond)
		defer client.Close()

		_, err = client.Execute("test")
		if err == nil {
			t.Error("expected error when server closes immediately")
		}
	})

	// 2. Server sends length header but closes before sending body -> ReadFull failure
	t.Run("truncated packet body", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()

		go func() {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()

			// Read client auth packet
			var l int32
			_ = binary.Read(conn, binary.LittleEndian, &l)
			d := make([]byte, l)
			_, _ = io.ReadFull(conn, d)

			// Send valid length (e.g. 20), but close immediately without sending the 20 bytes
			_ = binary.Write(conn, binary.LittleEndian, int32(20))
		}()

		client := NewClient(ln.Addr().String(), "pass", 500*time.Millisecond)
		defer client.Close()

		_, err = client.Execute("test")
		if err == nil {
			t.Error("expected error on truncated packet body")
		}
	})

	// 3. Server closes immediately after auth -> Execute command write failure
	t.Run("server closes right after auth", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()

		go func() {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Read client auth packet
			var l int32
			_ = binary.Read(conn, binary.LittleEndian, &l)
			d := make([]byte, l)
			_, _ = io.ReadFull(conn, d)

			// Send success auth response
			sendPacket(conn, 1, packetTypeAuthResponse, "")

			// Immediately close so next write (command) fails
			_ = conn.Close()
		}()

		client := NewClient(ln.Addr().String(), "pass", 100*time.Millisecond)
		defer client.Close()

		_, err = client.Execute("test")
		if err == nil {
			t.Error("expected error when server closes right after auth")
		}
	})
}

