// Package sse emits the Datastar v1.0 SSE wire protocol (datastar-patch-signals
// / datastar-patch-elements) directly onto a bufio.Writer. We hand-write frames
// instead of using datastar-go because that SDK is net/http and Fiber is fasthttp
// (its adaptor buffers, which breaks streaming). Frame layout mirrors the SDK's
// Send(): `event: <type>\n` then `data: <line>\n` per line, terminated by a blank
// line. The pinned browser bundle (datastar v1.0.2) consumes these.
package sse

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// PatchSignals merges a JSON signal object into the client store.
func PatchSignals(w *bufio.Writer, signals any) error {
	b, err := json.Marshal(signals)
	if err != nil {
		return err
	}
	slog.Debug("sse frame", "event", "datastar-patch-signals", "bytes", len(b), "signals", string(b))
	if _, err := fmt.Fprintf(w, "event: datastar-patch-signals\ndata: signals %s\n\n", b); err != nil {
		return err
	}
	return w.Flush()
}

// AppendElement appends an HTML fragment as a child of selector. html must be a
// single line (SSE data lines can't contain raw newlines).
func AppendElement(w *bufio.Writer, selector, html string) error {
	html = strings.ReplaceAll(html, "\n", " ")
	slog.Debug("sse frame", "event", "datastar-patch-elements", "mode", "append", "selector", selector, "bytes", len(html))
	if _, err := fmt.Fprintf(w,
		"event: datastar-patch-elements\ndata: mode append\ndata: selector %s\ndata: elements %s\n\n",
		selector, html,
	); err != nil {
		return err
	}
	return w.Flush()
}

func InnerElement(w *bufio.Writer, selector, html string) error {
	slog.Debug("sse frame", "event", "datastar-patch-elements", "mode", "inner", "selector", selector, "bytes", len(html))
	if _, err := fmt.Fprintf(w,
		"event: datastar-patch-elements\ndata: mode inner\ndata: selector %s\n", selector); err != nil {
		return err
	}
	for _, ln := range strings.Split(html, "\n") {
		if _, err := fmt.Fprintf(w, "data: elements %s\n", ln); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprint(w, "\n"); err != nil {
		return err
	}
	return w.Flush()
}
