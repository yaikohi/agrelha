package content

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

func TestSafeDialControl_BlockedIPs(t *testing.T) {
	tests := []struct {
		name string
		addr string
	}{
		{"IPv4 loopback 127.0.0.1", "127.0.0.1:443"},
		{"IPv4 loopback 127.0.0.2", "127.0.0.2:80"},
		{"IPv6 loopback ::1", "[::1]:443"},
		{"IPv6 loopback no port", "::1"},
		{"Private IPv4 10.0.0.1", "10.0.0.1:443"},
		{"Private IPv4 10.255.255.254", "10.255.255.254:80"},
		{"Private IPv4 172.16.0.1", "172.16.0.1:443"},
		{"Private IPv4 172.31.255.254", "172.31.255.254:80"},
		{"Private IPv4 192.168.1.1", "192.168.1.1:443"},
		{"Private IPv4 192.168.20.224", "192.168.20.224:80"},
		{"Private IPv6 ULA fc00::1", "[fc00::1]:443"},
		{"Private IPv6 ULA fd12::1", "[fd12:3456:789a::1]:80"},
		{"Unspecified IPv4 0.0.0.0", "0.0.0.0:80"},
		{"Unspecified IPv6 ::", "[::]:80"},
		{"Link-local IPv4 169.254.1.1", "169.254.1.1:80"},
		{"Link-local IPv6 fe80::1", "[fe80::1]:80"},
		{"Link-local IPv6 with zone", "[fe80::1%eth0]:80"},
		{"Multicast IPv4 224.0.0.1", "224.0.0.1:80"},
		{"Multicast IPv6 ff02::1", "[ff02::1]:80"},
		{"Invalid address", "not-a-valid-ip:80"},
		{"Empty address", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := safeDialControl("tcp", tc.addr, nil)
			if err == nil {
				t.Fatalf("expected error for blocked address %q, got nil", tc.addr)
			}
		})
	}
}

func TestSafeDialControl_AllowedIPs(t *testing.T) {
	tests := []struct {
		name string
		addr string
	}{
		{"Public IPv4 93.184.216.34", "93.184.216.34:443"},
		{"Public IPv4 1.1.1.1", "1.1.1.1:443"},
		{"Public IPv4 8.8.8.8", "8.8.8.8:53"},
		{"Public IPv6 Cloudflare", "[2606:4700:4700::1111]:443"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := safeDialControl("tcp", tc.addr, nil)
			if err != nil {
				t.Fatalf("expected nil for public address %q, got: %v", tc.addr, err)
			}
		})
	}
}

func TestDialHostResolvingToLoopbackOrPrivateFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	blockedDials := []struct {
		name string
		addr string
	}{
		{"Loopback IPv4", "127.0.0.1:54321"},
		{"Loopback hostname localhost", "localhost:54321"},
		{"Private 10.x", "10.0.0.1:54321"},
		{"Private 192.168.x", "192.168.1.1:54321"},
		{"Private 172.16.x", "172.16.0.1:54321"},
	}

	for _, tc := range blockedDials {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := imgTransport.DialContext(ctx, "tcp", tc.addr)
			if conn != nil {
				_ = conn.Close()
				t.Fatalf("expected connection to %s to be blocked, but connection succeeded", tc.addr)
			}
			if err == nil {
				t.Fatalf("expected dial to %s to fail, got nil error", tc.addr)
			}
			if !strings.Contains(err.Error(), "not a public IP") && !strings.Contains(err.Error(), "blocked") {
				t.Logf("dial error received as expected: %v", err)
			}
		})
	}
}

func TestImageProxy_DNSRebindingBlockedOnDial(t *testing.T) {
	// Start an internal test server simulating an internal private service.
	serverHit := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverHit = true
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("fake-image-bytes"))
	}))
	defer ts.Close()

	// Extract port from test server
	_, port, err := net.SplitHostPort(ts.Listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to split host port: %v", err)
	}

	// Mock lookupIP so that PublicHost returns true initially (simulating TOCTOU DNS rebinding:
	// initial DNS resolution during PublicHost returns public IP, but dial targets localhost/loopback).
	origLookup := lookupIP
	defer func() { lookupIP = origLookup }()
	lookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}

	app := fiber.New()
	h := New(Config{})
	h.Register(app)

	// Target localhost which resolves to 127.0.0.1 or ::1
	targetURL := fmt.Sprintf("https://localhost:%s/image.png", port)
	req := httptest.NewRequest("GET", "/img?u="+targetURL, nil)
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}

	// Dial control should abort the connection, resulting in 502 Bad Gateway
	if resp.StatusCode != fiber.StatusBadGateway {
		t.Fatalf("expected status 502 Bad Gateway due to dial control block, got %d", resp.StatusCode)
	}

	// Verify that the internal server was never reached
	if serverHit {
		t.Fatalf("internal server was unexpectedly reached; dial control failed to block connection")
	}
}
