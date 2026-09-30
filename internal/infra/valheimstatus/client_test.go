package valheimstatus

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const realStatus = `{"last_status_update": "2026-09-28T21:00:01.521344+00:00", "error": null, "server_name": "boppo", "server_type": "d", "platform": "l", "player_count": 0, "password_protected": true, "vac_enabled": false, "port": 2456, "steam_id": 90293633037725706, "keywords": "g=1.0.16,n=40,m=", "game_id": 892970, "players": []}`

func clientFor(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New("valheim", WithURL(func(string) string { return srv.URL }))
}

func TestReadsTheRealStatusPayload(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(realStatus))
	})
	st, err := c.Status(context.Background(), "valheim-boppo-02")
	if err != nil {
		t.Fatal(err)
	}
	if st.ServerName != "boppo" || st.PlayerCount != 0 {
		t.Errorf("parsed wrong: %+v", st)
	}
	players, known := c.Players(context.Background(), "valheim-boppo-02")
	if !known || players != 0 {
		t.Errorf("an idle server is known and empty, got %d known=%v", players, known)
	}
}

func TestCountsConnectedPlayers(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"error": null, "server_name": "boppo", "player_count": 3, "players": []}`))
	})
	players, known := c.Players(context.Background(), "svc")
	if !known || players != 3 {
		t.Errorf("got %d known=%v, want 3 known", players, known)
	}
}

func TestUnreachableServerIsUnknownNotEmpty(t *testing.T) {
	c := New("valheim", WithURL(func(string) string { return "http://127.0.0.1:1/status.json" }))
	if players, known := c.Players(context.Background(), "svc"); known {
		t.Errorf("an unreachable server must be unknown, got %d known=%v", players, known)
	}
}

func TestNonOKResponseIsUnknown(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	if _, known := c.Players(context.Background(), "svc"); known {
		t.Error("a 503 is not an answer")
	}
	if _, err := c.Status(context.Background(), "svc"); err == nil {
		t.Error("expected an error for a non-200")
	}
}

func TestGarbageBodyIsUnknown(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json at all"))
	})
	if _, known := c.Players(context.Background(), "svc"); known {
		t.Error("an unparseable body is not an answer")
	}
}

func TestAnImageReportedErrorIsNotAnEmptyServer(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"error": "a2s query timed out", "server_name": "boppo", "player_count": 0}`))
	})
	if players, known := c.Players(context.Background(), "svc"); known {
		t.Errorf("a self-reported error must not read as empty, got %d known=%v", players, known)
	}
}

func TestNoServiceNameIsRefused(t *testing.T) {
	c := New("valheim")
	if _, err := c.Status(context.Background(), ""); err == nil {
		t.Error("expected an error with no service to query")
	}
	var nilClient *Client
	if _, err := nilClient.Status(context.Background(), "svc"); err == nil {
		t.Error("a nil client must not panic or claim success")
	}
	if _, known := nilClient.Players(context.Background(), "svc"); known {
		t.Error("a nil client knows nothing")
	}
}

func TestDefaultURLTargetsTheDeclaredPort(t *testing.T) {
	c := New("valheim")
	got := c.urlFn("valheim-boppo-02")
	want := "http://valheim-boppo-02.valheim.svc.cluster.local:9001/status.json"
	if got != want {
		t.Errorf("url = %q, want %q", got, want)
	}
}
