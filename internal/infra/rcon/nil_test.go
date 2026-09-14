package rcon

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestNilClientDoesNotPanic(t *testing.T) {
	var c *Client
	if _, err := c.Execute("/list"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Execute on nil client: got %v, want ErrNotConfigured", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close on nil client: %v", err)
	}
}

func startMockRCONServer(t *testing.T, password string) (string, func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go handleMockRCONConn(conn, password)
		}
	}()

	return l.Addr().String(), func() {
		_ = l.Close()
		<-done
	}
}

func handleMockRCONConn(conn net.Conn, correctPassword string) {
	defer conn.Close()
	for {
		var length int32
		if err := binary.Read(conn, binary.LittleEndian, &length); err != nil {
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
			respID := id
			if body != correctPassword {
				respID = -1
			}
			sendMockPacket(conn, respID, packetTypeAuthResponse, "")
		case packetTypeExecCommand:
			resp := "ok: " + body
			sendMockPacket(conn, id, packetTypeResponse, resp)
		}
	}
}

func sendMockPacket(conn net.Conn, id, typ int32, body string) {
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

func TestRCONClientAndPool(t *testing.T) {
	addr, closeServer := startMockRCONServer(t, "secretpass")
	defer closeServer()

	// 1. Success with Pool and ClientFor
	pool := NewPool("secretpass", 2*time.Second)
	client := pool.ClientFor(addr)
	cached := pool.ClientFor(addr)
	if client != cached {
		t.Errorf("expected cached client from pool")
	}

	resp, err := client.Execute("whitelist list")
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if resp != "ok: whitelist list" {
		t.Errorf("execute resp = %q, want 'ok: whitelist list'", resp)
	}

	// Repeated execute on open conn
	resp2, err := client.Execute("save-all")
	if err != nil || resp2 != "ok: save-all" {
		t.Errorf("execute2 resp = %q, err = %v", resp2, err)
	}

	// Close
	if err := client.Close(); err != nil {
		t.Errorf("close failed: %v", err)
	}
	// Close again (noop)
	if err := client.Close(); err != nil {
		t.Errorf("second close failed: %v", err)
	}

	// 2. Auth failure
	badClient := NewClient(addr, "wrongpassword", 2*time.Second)
	_, err = badClient.Execute("list")
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("expected auth failure error, got: %v", err)
	}

	// 3. Dial failure
	deadClient := NewClient("127.0.0.1:1", "secretpass", 100*time.Millisecond)
	_, err = deadClient.Execute("list")
	if err == nil {
		t.Errorf("expected dial error on closed port")
	}
}

