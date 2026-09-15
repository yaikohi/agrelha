package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"agrelha/internal/domain"
)

func TestIncidentRoundTrip(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "inc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()

	if got, err := st.LastIncident(ctx, domain.GameMinecraft, 3); err != nil || got != nil {
		t.Fatalf("no incidents yet: got %v, %v", got, err)
	}

	older := domain.Incident{GameID: domain.GameMinecraft, Number: 3, At: time.Now().Add(-time.Hour), ExitCode: 1, Reason: "Error"}
	newer := domain.Incident{
		GameID: domain.GameMinecraft, Number: 3, At: time.Now(),
		RestartCount: 10, Reason: "CrashLoopBackOff", OOMKilled: true,
		LogTail: "java.lang.OutOfMemoryError",
	}
	for _, in := range []domain.Incident{older, newer} {
		if _, err := st.RecordIncident(ctx, in); err != nil {
			t.Fatalf("RecordIncident: %v", err)
		}
	}

	got, err := st.LastIncident(ctx, domain.GameMinecraft, 3)
	if err != nil || got == nil {
		t.Fatalf("LastIncident: %v, %v", got, err)
	}
	if !got.OOMKilled || got.RestartCount != 10 {
		t.Errorf("LastIncident returned the older row: %+v", got)
	}
	if got.LogTail == "" {
		t.Error("log tail must survive: it is the only record of why the server died")
	}

	if all, err := st.ListIncidents(ctx, domain.GameMinecraft, 3, 10); err != nil || len(all) != 2 {
		t.Errorf("ListIncidents = %d rows, %v", len(all), err)
	}
	if other, err := st.ListIncidents(ctx, domain.GameValheim, 3, 10); err != nil || len(other) != 0 {
		t.Errorf("incidents must be scoped per game: got %d for valheim", len(other))
	}

	// Test RecordIncident with zero time and ListIncidents with limit <= 0
	id, err := st.RecordIncident(ctx, domain.Incident{
		GameID: domain.GameMinecraft,
		Number: 3,
		Reason: "SyntheticZeroTime",
	})
	if err != nil || id == 0 {
		t.Fatalf("RecordIncident with zero time failed: %v", err)
	}
	defaultLimitList, err := st.ListIncidents(ctx, domain.GameMinecraft, 3, 0)
	if err != nil || len(defaultLimitList) != 3 {
		t.Fatalf("ListIncidents with limit 0 should default to 20, got count %d, err=%v", len(defaultLimitList), err)
	}
}

func TestIncidentSummary(t *testing.T) {
	cases := []struct {
		in   domain.Incident
		want string
	}{
		{domain.Incident{OOMKilled: true}, "Out of memory"},
		{domain.Incident{Reason: "CrashLoopBackOff", RestartCount: 7}, "restarted 7 times"},
		{domain.Incident{ExitCode: 137}, "code 137"},
		{domain.Incident{}, "Stopped unexpectedly"},
	}
	for _, c := range cases {
		if got := c.in.Summary(); got == "" || !contains(got, c.want) {
			t.Errorf("Summary(%+v) = %q, want it to mention %q", c.in, got, c.want)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})())
}
