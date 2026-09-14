package sse

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"
)

type errWriter struct {
	err error
}

func (e *errWriter) Write(p []byte) (n int, err error) {
	return 0, e.err
}

type mockWriter struct {
	failAt int
	calls  int
	err    error
}

func (m *mockWriter) Write(p []byte) (int, error) {
	m.calls++
	if m.failAt > 0 && m.calls >= m.failAt {
		return 0, m.err
	}
	return len(p), nil
}

func TestPatchSignals(t *testing.T) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	signals := map[string]any{"connected": true, "count": 42}
	if err := PatchSignals(w, signals); err != nil {
		t.Fatalf("unexpected PatchSignals error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "event: datastar-patch-signals\n") {
		t.Errorf("expected datastar-patch-signals event, got %q", out)
	}
	if !strings.Contains(out, "data: signals {\"connected\":true,\"count\":42}\n\n") {
		t.Errorf("expected signals data, got %q", out)
	}

	// Marshal error
	if err := PatchSignals(w, make(chan int)); err == nil {
		t.Errorf("expected error marshaling channel")
	}

	// Write / Flush error
	ew := bufio.NewWriterSize(&errWriter{err: errors.New("write fail")}, 10)
	if err := PatchSignals(ew, signals); err == nil {
		t.Errorf("expected error on failing writer")
	}
}

func TestAppendElement(t *testing.T) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	if err := AppendElement(w, "#logs", "<div>line 1\nline 2</div>"); err != nil {
		t.Fatalf("unexpected AppendElement error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "event: datastar-patch-elements\ndata: mode append\ndata: selector #logs\ndata: elements <div>line 1 line 2</div>\n\n") {
		t.Errorf("unexpected output: %q", out)
	}

	// Write / Flush error
	ew := bufio.NewWriterSize(&errWriter{err: errors.New("write fail")}, 10)
	if err := AppendElement(ew, "#logs", "hello"); err == nil {
		t.Errorf("expected error on failing writer")
	}
}

func TestInnerElement(t *testing.T) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	html := "<h1>Header</h1>\n<p>Body</p>"
	if err := InnerElement(w, "#content", html); err != nil {
		t.Fatalf("unexpected InnerElement error: %v", err)
	}

	out := buf.String()
	expected := "event: datastar-patch-elements\ndata: mode inner\ndata: selector #content\ndata: elements <h1>Header</h1>\ndata: elements <p>Body</p>\n\n"
	if out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}

	// 1. Initial write failure
	mw1 := &mockWriter{failAt: 1, err: errors.New("initial write fail")}
	ew1 := bufio.NewWriterSize(mw1, 16)
	if err := InnerElement(ew1, "#content", html); err == nil {
		t.Errorf("expected error on initial write")
	}

	// 2. Loop line failure (buffer 80 holds 72 bytes, loop line of 21 overflows and fails)
	mw2 := &mockWriter{failAt: 1, err: errors.New("fail during loop")}
	ew2 := bufio.NewWriterSize(mw2, 80)
	if err := InnerElement(ew2, "#content", "line1"); err == nil {
		t.Errorf("expected error during lines loop")
	}

	// 3. Final newline failure (buffer of 93 is full, Fprint flushes and fails)
	mw3 := &mockWriter{failAt: 1, err: errors.New("fail on newline")}
	ew3 := bufio.NewWriterSize(mw3, 93)
	if err := InnerElement(ew3, "#content", "short"); err == nil {
		t.Errorf("expected error on newline")
	}

	// 4. Final flush failure (buffer of 200 holds all 94 bytes, Flush fails)
	mw4 := &mockWriter{failAt: 1, err: errors.New("fail on flush")}
	ew4 := bufio.NewWriterSize(mw4, 200)
	if err := InnerElement(ew4, "#content", "short"); err == nil {
		t.Errorf("expected error on flush")
	}
}
