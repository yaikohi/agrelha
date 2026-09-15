// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestRun_InvalidDB(t *testing.T) {
	t.Setenv("DB_PATH", "/dev/null/impossible/path/db.sqlite")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	code := run(context.Background(), stdout, stderr, nil)
	if code != 1 {
		t.Fatalf("expected exit code 1 for invalid DB path, got %d", code)
	}
}

func TestRun_InvalidListenAddr(t *testing.T) {
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "agrelha.db"))
	t.Setenv("LISTEN_ADDR", "999.999.999.999:99999")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	code := run(context.Background(), stdout, stderr, nil)
	if code != 1 {
		t.Fatalf("expected exit code 1 for invalid listen addr, got %d", code)
	}
}

func TestRun_GracefulShutdown(t *testing.T) {
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "agrelha.db"))
	t.Setenv("LISTEN_ADDR", "127.0.0.1:0")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	code := run(ctx, stdout, stderr, nil)
	if code != 0 {
		t.Fatalf("expected exit code 0 for graceful shutdown, got %d", code)
	}
}

func TestMain_ExitCode(t *testing.T) {
	origExit := osExit
	defer func() { osExit = origExit }()

	var exitCode int
	osExit = func(code int) {
		exitCode = code
	}

	t.Setenv("DB_PATH", "/dev/null/impossible/path/db.sqlite")
	main()

	if exitCode != 1 {
		t.Fatalf("expected main to exit with code 1, got %d", exitCode)
	}
}

func TestRun_ForcedShutdownError(t *testing.T) {
	origTimeout := shutdownTimeout
	defer func() { shutdownTimeout = origTimeout }()
	shutdownTimeout = 1 * time.Millisecond

	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "agrelha.db"))
	t.Setenv("LISTEN_ADDR", "127.0.0.1:28392")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	ctx, cancel := context.WithCancel(context.Background())
	var activeConn net.Conn
	defer func() {
		if activeConn != nil {
			activeConn.Close()
		}
	}()

	go func() {
		for {
			c, err := net.Dial("tcp", "127.0.0.1:28392")
			if err == nil {
				activeConn = c
				_, _ = activeConn.Write([]byte("GET /sse HTTP/1.1\r\nHost: localhost\r\nAccept: text/event-stream\r\n\r\n"))
				buf := make([]byte, 32)
				_, _ = activeConn.Read(buf)
				cancel()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	code := run(ctx, stdout, stderr, nil)
	if code != 1 {
		t.Fatalf("expected exit code 1 for forced shutdown error, got %d", code)
	}
}
