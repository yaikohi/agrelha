package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"agrelha/internal/ports"
)

func TestNewSocketClient_EnvironmentAndOptions(t *testing.T) {
	// Test DefaultSocketPath
	t.Setenv("DOCKER_HOST", "")
	c1 := NewSocketClient("")
	if c1.baseURL != "http://localhost" {
		t.Errorf("expected baseURL http://localhost, got %s", c1.baseURL)
	}

	// Test DOCKER_HOST with unix://
	t.Setenv("DOCKER_HOST", "unix:///tmp/test-docker.sock")
	c2 := NewSocketClient("")
	if c2 == nil {
		t.Fatal("expected non-nil client")
	}

	// Test DialContext invocation on transport
	tr, ok := c2.http.Transport.(*http.Transport)
	if !ok || tr.DialContext == nil {
		t.Fatal("expected http.Transport with DialContext")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _ = tr.DialContext(ctx, "unix", "/tmp/nonexistent-docker-test.sock")
}

func TestSocketClient_ContainerStart_ErrorsAndCodes(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
	}{
		{name: "200 OK", statusCode: http.StatusOK},
		{name: "204 No Content", statusCode: http.StatusNoContent},
		{name: "304 Not Modified", statusCode: http.StatusNotModified},
		{name: "500 Error", statusCode: http.StatusInternalServerError, body: "daemon failure", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			c := NewSocketClient("", WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
			err := c.ContainerStart(ctx, "test-box")
			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestSocketClient_ContainerStop_ErrorsAndCodes(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
	}{
		{name: "200 OK", statusCode: http.StatusOK},
		{name: "204 No Content", statusCode: http.StatusNoContent},
		{name: "304 Not Modified", statusCode: http.StatusNotModified},
		{name: "500 Error", statusCode: http.StatusInternalServerError, body: "stop timeout", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			c := NewSocketClient("", WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
			err := c.ContainerStop(ctx, "test-box")
			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestSocketClient_ContainerRestart_ErrorsAndCodes(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
	}{
		{name: "200 OK", statusCode: http.StatusOK},
		{name: "204 No Content", statusCode: http.StatusNoContent},
		{name: "500 Error", statusCode: http.StatusInternalServerError, body: "restart failed", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			c := NewSocketClient("", WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
			err := c.ContainerRestart(ctx, "test-box")
			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestSocketClient_ContainerInspect_ErrorsAndCodes(t *testing.T) {
	ctx := context.Background()

	t.Run("404 Not Found returns ErrNotExist", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer srv.Close()

		c := NewSocketClient("", WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
		_, err := c.ContainerInspect(ctx, "missing")
		if !os.IsNotExist(err) {
			t.Errorf("expected os.ErrNotExist, got %v", err)
		}
	})

	t.Run("500 Server Error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal crash", http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := NewSocketClient("", WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
		_, err := c.ContainerInspect(ctx, "c1")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("200 with invalid json", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{not-json}"))
		}))
		defer srv.Close()

		c := NewSocketClient("", WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
		_, err := c.ContainerInspect(ctx, "c1")
		if err == nil {
			t.Errorf("expected JSON decode error, got nil")
		}
	})
}

func TestSocketClient_ContainerStats_ErrorsAndCodes(t *testing.T) {
	ctx := context.Background()

	t.Run("500 Server Error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "stats stream error", http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := NewSocketClient("", WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
		_, err := c.ContainerStats(ctx, "c1")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("200 with invalid json", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{invalid-json}"))
		}))
		defer srv.Close()

		c := NewSocketClient("", WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
		_, err := c.ContainerStats(ctx, "c1")
		if err == nil {
			t.Errorf("expected JSON decode error, got nil")
		}
	})
}

func TestSocketClient_ContainerLogs(t *testing.T) {
	ctx := context.Background()

	t.Run("success with default tail and follow", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("tail") != "200" {
				t.Errorf("expected tail 200, got %s", r.URL.Query().Get("tail"))
			}
			if r.URL.Query().Get("follow") != "1" {
				t.Errorf("expected follow 1, got %s", r.URL.Query().Get("follow"))
			}
			// Write multiplexed frame
			var header [8]byte
			header[0] = 1 // stdout
			binary.BigEndian.PutUint32(header[4:8], 4)
			_, _ = w.Write(header[:])
			_, _ = w.Write([]byte("ping"))
		}))
		defer srv.Close()

		c := NewSocketClient("", WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
		rc, err := c.ContainerLogs(ctx, "c1", ports.LogOptions{Tail: 0, Follow: true})
		if err != nil {
			t.Fatalf("ContainerLogs failed: %v", err)
		}
		defer rc.Close()

		out, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("ReadAll failed: %v", err)
		}
		if string(out) != "ping" {
			t.Errorf("got %q, want 'ping'", string(out))
		}
	})

	t.Run("500 Server Error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "logs unavailable", http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := NewSocketClient("", WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
		_, err := c.ContainerLogs(ctx, "c1", ports.LogOptions{Tail: 10})
		if err == nil {
			t.Errorf("expected error on 500, got nil")
		}
	})
}

func TestDemuxLogReader_EdgeCases(t *testing.T) {
	t.Run("fallback raw non-multiplexed header", func(t *testing.T) {
		raw := []byte("12345678abcdefgh")
		reader := newDemuxLogReader(io.NopCloser(bytes.NewReader(raw)))
		defer reader.Close()

		out, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("ReadAll failed: %v", err)
		}
		if string(out) != string(raw) {
			t.Errorf("got %q, want %q", string(out), string(raw))
		}
	})

	t.Run("short read less than 8 bytes returns EOF", func(t *testing.T) {
		raw := []byte("short")
		reader := newDemuxLogReader(io.NopCloser(bytes.NewReader(raw)))
		defer reader.Close()

		p := make([]byte, 10)
		n, err := reader.Read(p)
		if err != io.EOF || n != 0 {
			t.Errorf("expected 0, io.EOF; got %d, %v", n, err)
		}
	})

	t.Run("empty frameLen does not crash", func(t *testing.T) {
		var header [8]byte
		header[0] = 1
		binary.BigEndian.PutUint32(header[4:8], 0)
		reader := newDemuxLogReader(io.NopCloser(bytes.NewReader(header[:])))
		defer reader.Close()

		p := make([]byte, 10)
		n, err := reader.Read(p)
		if err != io.EOF && err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != 0 {
			t.Errorf("expected 0 bytes, got %d", n)
		}
	})

	t.Run("small buffer reads consume from r.buf", func(t *testing.T) {
		var raw bytes.Buffer
		var header [8]byte
		header[0] = 1
		binary.BigEndian.PutUint32(header[4:8], 10)
		raw.Write(header[:])
		raw.WriteString("0123456789")

		reader := newDemuxLogReader(io.NopCloser(&raw))
		defer reader.Close()

		// Read 4 bytes
		buf := make([]byte, 4)
		n, err := reader.Read(buf)
		if err != nil || n != 4 || string(buf) != "0123" {
			t.Fatalf("first read got n=%d, err=%v, s=%q", n, err, string(buf))
		}
		// Read next 4 bytes (from r.buf)
		n, err = reader.Read(buf)
		if err != nil || n != 4 || string(buf) != "4567" {
			t.Fatalf("second read got n=%d, err=%v, s=%q", n, err, string(buf))
		}
		// Read remaining 2 bytes
		n, err = reader.Read(buf)
		if err != nil || n != 2 || string(buf[:2]) != "89" {
			t.Fatalf("third read got n=%d, err=%v, s=%q", n, err, string(buf[:2]))
		}
	})

	t.Run("header read error returns error", func(t *testing.T) {
		reader := newDemuxLogReader(&failingReader{err: errors.New("read failed")})
		defer reader.Close()
		p := make([]byte, 10)
		_, err := reader.Read(p)
		if err == nil || !strings.Contains(err.Error(), "read failed") {
			t.Errorf("expected read failed error, got %v", err)
		}
	})

	t.Run("payload copy error returns error", func(t *testing.T) {
		reader := newDemuxLogReader(&headerThenErrorReader{})
		defer reader.Close()
		p := make([]byte, 10)
		_, err := reader.Read(p)
		if err == nil || !strings.Contains(err.Error(), "payload failure") {
			t.Errorf("expected payload failure error, got %v", err)
		}
	})
}

type failingReader struct {
	err error
}

func (f *failingReader) Read([]byte) (int, error) { return 0, f.err }
func (f *failingReader) Close() error             { return nil }

type headerThenErrorReader struct {
	sentHeader bool
}

func (h *headerThenErrorReader) Read(p []byte) (int, error) {
	if !h.sentHeader {
		var hdr [8]byte
		hdr[0] = 1
		binary.BigEndian.PutUint32(hdr[4:8], 50)
		copy(p, hdr[:])
		h.sentHeader = true
		return 8, nil
	}
	return 0, errors.New("payload failure")
}

func (h *headerThenErrorReader) Close() error { return nil }

type errRoundTripper struct{}

func (errRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("network connection refused")
}

func TestSocketClient_TransportErrors(t *testing.T) {
	ctx := context.Background()
	c := NewSocketClient("", WithHTTPClient(&http.Client{Transport: errRoundTripper{}}))

	if err := c.ContainerStart(ctx, "c1"); err == nil {
		t.Error("ContainerStart expected transport error, got nil")
	}
	if err := c.ContainerStop(ctx, "c1"); err == nil {
		t.Error("ContainerStop expected transport error, got nil")
	}
	if err := c.ContainerRestart(ctx, "c1"); err == nil {
		t.Error("ContainerRestart expected transport error, got nil")
	}
	if _, err := c.ContainerInspect(ctx, "c1"); err == nil {
		t.Error("ContainerInspect expected transport error, got nil")
	}
	if _, err := c.ContainerStats(ctx, "c1"); err == nil {
		t.Error("ContainerStats expected transport error, got nil")
	}
	if _, err := c.ContainerLogs(ctx, "c1", ports.LogOptions{}); err == nil {
		t.Error("ContainerLogs expected transport error, got nil")
	}
}

