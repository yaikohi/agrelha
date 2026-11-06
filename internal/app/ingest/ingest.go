// Package ingest tails the valheim pod logs and turns connection/character lines
// into the persistent player roster + join events. Runs as a background goroutine;
// it reconnects across pod restarts. Independent of the SSE live-tail (which opens
// its own log stream).
package ingest

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"
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

	activeMu      sync.RWMutex
	activePlayers = make(map[string]bool)
)

func activeKey(instance int, id string) string {
	return fmt.Sprintf("%d:%s", instance, id)
}

func isPlayerActive(instance int, id string) bool {
	activeMu.RLock()
	defer activeMu.RUnlock()
	return activePlayers[activeKey(instance, id)]
}

func setPlayerActive(instance int, id string, active bool) {
	activeMu.Lock()
	defer activeMu.Unlock()
	key := activeKey(instance, id)
	if active {
		activePlayers[key] = true
	} else {
		delete(activePlayers, key)
	}
}

func clearActivePresence(instance int) {
	activeMu.Lock()
	defer activeMu.Unlock()
	prefix := fmt.Sprintf("%d:", instance)
	for k := range activePlayers {
		if strings.HasPrefix(k, prefix) {
			delete(activePlayers, k)
		}
	}
}

type presenceStore interface {
	ClearPresence(instance int) error
	UpsertSeen(instance int, id, character string, joined bool) error
	SetOnline(instance int, id string, online bool) error
	RecordEvent(kind, id string) error
}

const (
	RetryDelay    = 3 * time.Second
	MaxRetryDelay = 5 * time.Minute
)

func Run(ctx context.Context, instance int, k logStreamer, st presenceStore, delays ...time.Duration) {
	clearActivePresence(instance)
	_ = st.ClearPresence(instance)
	defer func() {
		clearActivePresence(instance)
		_ = st.ClearPresence(instance)
	}()
	delay := RetryDelay
	maxDelay := MaxRetryDelay
	if len(delays) > 0 && delays[0] > 0 {
		delay = delays[0]
	}
	if len(delays) > 1 && delays[1] > 0 {
		maxDelay = delays[1]
	}
	initialDelay := delay
	for {
		if ctx.Err() != nil {
			return
		}
		err := consume(ctx, instance, k, st)
		if err == nil {
			delay = initialDelay
		} else if ctx.Err() == nil {
			slog.Warn("ingest: log stream ended, retrying", "instance", instance, "err", err, "retry_in", delay)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if err != nil {
			if delay *= 2; delay > maxDelay {
				delay = maxDelay
			}
		}
	}
}

func consume(ctx context.Context, instance int, k logStreamer, st presenceStore) error {
	rc, err := k.StreamLogs(ctx, 200)
	if err != nil {
		return err
	}
	defer rc.Close()

	var lastSteam string
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := sc.Text()
		switch {
		case steamRe.MatchString(line):
			lastSteam = steamRe.FindStringSubmatch(line)[1]
			if !isPlayerActive(instance, lastSteam) {
				setPlayerActive(instance, lastSteam, true)
				_ = st.UpsertSeen(instance, lastSteam, "", true)
				_ = st.RecordEvent("join", lastSteam)
			}
			_ = st.SetOnline(instance, lastSteam, true)
		case charRe.MatchString(line):
			if lastSteam != "" {
				_ = st.UpsertSeen(instance, lastSteam, charRe.FindStringSubmatch(line)[1], false)
			}
		case dcRe.MatchString(line):
			id := dcRe.FindStringSubmatch(line)[1]
			wasActive := isPlayerActive(instance, id)
			setPlayerActive(instance, id, false)
			_ = st.SetOnline(instance, id, false)
			if wasActive {
				_ = st.RecordEvent("leave", id)
			}
		}
	}
	return sc.Err()
}
