// Package ingest tails the valheim pod logs and turns connection/character lines
// into the persistent player roster + join events. Runs as a background goroutine;
// it reconnects across pod restarts. Independent of the SSE live-tail (which opens
// its own log stream).
package ingest

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"regexp"
	"time"
)

type logStreamer interface {
	StreamLogs(ctx context.Context, tail int64) (io.ReadCloser, error)
}

var (
	// lloesche/Valheim server log lines.
	steamRe = regexp.MustCompile(`Got connection SteamID (\d{17})`)
	charRe  = regexp.MustCompile(`Got character ZDOID from (.+?) :`)
	dcRe    = regexp.MustCompile(`Closing socket (\d{17})`)
)

type presenceStore interface {
	ClearPresence() error
	UpsertSeen(id, character string, joined bool) error
	SetOnline(id string, online bool) error
	RecordEvent(kind, id string) error
}

var (
	retryDelay    = 3 * time.Second
	maxRetryDelay = 5 * time.Minute
)

func Run(ctx context.Context, k logStreamer, st presenceStore) {
	_ = st.ClearPresence()
	delay := retryDelay
	for {
		if ctx.Err() != nil {
			return
		}
		err := consume(ctx, k, st)
		if err == nil {
			delay = retryDelay
		} else if ctx.Err() == nil {
			slog.Warn("ingest: log stream ended, retrying", "err", err, "retry_in", delay)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay): // the server may be restarting
		}
		if err != nil {
			if delay *= 2; delay > maxRetryDelay {
				delay = maxRetryDelay
			}
		}
	}
}

func consume(ctx context.Context, k logStreamer, st presenceStore) error {
	rc, err := k.StreamLogs(ctx, 200)
	if err != nil {
		return err
	}
	defer rc.Close()

	var lastSteam string
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case steamRe.MatchString(line):
			lastSteam = steamRe.FindStringSubmatch(line)[1]
			_ = st.UpsertSeen(lastSteam, "", true)
			_ = st.SetOnline(lastSteam, true)
			_ = st.RecordEvent("join", lastSteam)
		case charRe.MatchString(line):
			if lastSteam != "" {
				_ = st.UpsertSeen(lastSteam, charRe.FindStringSubmatch(line)[1], false)
			}
		case dcRe.MatchString(line):
			id := dcRe.FindStringSubmatch(line)[1]
			_ = st.SetOnline(id, false)
			_ = st.RecordEvent("leave", id)
		}
	}
	return sc.Err()
}
