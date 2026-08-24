// Package valheim reads the lloesche status.json endpoint (STATUS_HTTP=true).
package valheim

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Status struct {
	PlayerCount int
	Online      bool
	Err         string // e.g. the known A2S "TimeoutError" the server currently returns
}

// FetchStatus GETs status.json. The endpoint returns either
// {"player_count":N,...} or {"error":"TimeoutError(...)"} when the server's A2S
// self-query fails — we surface that rather than pretending 0 players.
func FetchStatus(ctx context.Context, url string) (Status, error) {
	if url == "" {
		return Status{}, fmt.Errorf("no status url configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Status{}, err
	}
	defer resp.Body.Close()

	var raw struct {
		PlayerCount *int   `json:"player_count"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Status{}, err
	}
	if raw.Error != "" {
		return Status{Err: raw.Error}, nil
	}
	s := Status{Online: true}
	if raw.PlayerCount != nil {
		s.PlayerCount = *raw.PlayerCount
	}
	return s, nil
}
