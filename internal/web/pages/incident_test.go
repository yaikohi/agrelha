package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"agrelha/internal/domain"
)

func TestIncidentViewIsNilWhenNothingHappened(t *testing.T) {
	if IncidentView(nil) != nil {
		t.Error("no incident must render nothing, so the panel can branch on presence")
	}
}

func TestIncidentViewCarriesTheCause(t *testing.T) {
	got := IncidentView(&domain.Incident{
		At:           time.Date(2026, 9, 11, 15, 4, 0, 0, time.UTC),
		RestartCount: 10,
		ExitCode:     137,
		Reason:       "CrashLoopBackOff",
		OOMKilled:    true,
		LogTail:      "java.lang.OutOfMemoryError",
	})
	if got == nil {
		t.Fatal("expected a view")
	}
	if got.Summary == "" {
		t.Error("the operator needs a one-line cause, not raw fields")
	}
	if !got.OOMKilled || got.RestartCount != 10 || got.ExitCode != 137 {
		t.Errorf("detail lost: %+v", got)
	}
	if got.LogTail == "" {
		t.Error("the captured log is the whole point of recording the incident")
	}
	if got.When != "2026-09-11 15:04" {
		t.Errorf("When = %q", got.When)
	}
}

func TestIncidentPanelRendersTheCause(t *testing.T) {
	in := &IncidentUI{
		Summary: "Out of memory — the server exceeded its tier's limit",
		When:    "2026-09-11 15:04", RestartCount: 10, ExitCode: 137,
		OOMKilled: true, LogTail: "Caused by: java.lang.NoClassDefFoundError",
	}
	var buf bytes.Buffer
	if err := IncidentPanel(in).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, want := range []string{"Last failure", "Out of memory", "10 restarts", "exit 137", "NoClassDefFoundError", "2026-09-11 15:04"} {
		if !strings.Contains(body, want) {
			t.Errorf("panel missing %q", want)
		}
	}
}

func TestIncidentPanelRendersNothingWithoutAnIncident(t *testing.T) {
	var buf bytes.Buffer
	if err := IncidentPanel(nil).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(buf.String()) != "" {
		t.Errorf("a healthy instance must show no failure panel, got: %q", buf.String())
	}
}

func TestIncidentPanelSaysWhenNoLogWasCaptured(t *testing.T) {
	var buf bytes.Buffer
	_ = IncidentPanel(&IncidentUI{Summary: "Exited with code 1", When: "now"}).Render(context.Background(), &buf)
	if !strings.Contains(buf.String(), "No log was captured") {
		t.Error("an empty log tail must be explained, not shown as a blank box")
	}
}
