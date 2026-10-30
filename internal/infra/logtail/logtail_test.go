package logtail

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeLines(t *testing.T, path string, from, to int) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for i := from; i <= to; i++ {
		fmt.Fprintf(f, "line %d\n", i)
	}
}

func TestFollowStartsFromTheNewestLinesNotTheOldest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "console.log")
	writeLines(t, path, 1, 500)

	ctx := t.Context()
	rc, err := Follow(ctx, path, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()

	buf := make([]byte, 4096)
	n, _ := rc.Read(buf)
	got := string(buf[:n])

	if strings.Contains(got, "line 1\n") {
		t.Errorf(`the tail began at the start of the file.

A console that has been running for days opens on its first line, which is why the operator has to
scroll for ages to reach anything current. Only the last lines should be sent.
got: %.80q`, got)
	}
	if !strings.Contains(got, "line 500") {
		t.Errorf("the newest line is missing from the tail: %.120q", got)
	}
}

func TestFollowDeliversLinesAppendedAfterItStarted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "console.log")
	writeLines(t, path, 1, 3)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rc, err := Follow(ctx, path, 5)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(rc)
		done <- string(b)
	}()

	time.Sleep(200 * time.Millisecond)
	writeLines(t, path, 4, 5)
	time.Sleep(1500 * time.Millisecond)
	cancel()

	select {
	case got := <-done:
		if !strings.Contains(got, "line 5") {
			t.Errorf(`lines appended after the stream opened never arrived.

A log tab that shows a frozen snapshot is not a live console.
got: %.160q`, got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reader never finished after cancel")
	}
}

func TestAMissingFileIsAnErrorNotAnEmptyStream(t *testing.T) {
	_, err := Follow(context.Background(), filepath.Join(t.TempDir(), "nope.log"), 10)
	if err == nil {
		t.Error(`a missing console log returned a stream rather than an error.

The caller falls back to the runtime stream when the file is absent; a silent empty reader would
show the operator a blank pane and no reason for it.`)
	}
}

func TestAnEmptyFileDoesNotBreakTheTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "console.log")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if _, err := Follow(ctx, path, 10); err != nil {
		t.Errorf("a freshly created console log failed to open: %v", err)
	}
}
