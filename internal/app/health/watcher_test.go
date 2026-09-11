package health

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

type fakeSource struct {
	game      domain.GameID
	instances []domain.Instance
	status    map[int]ports.Status
	logs      map[int]string
	logCalls  int
}

func (f *fakeSource) GameID() domain.GameID { return f.game }
func (f *fakeSource) ListInstances(context.Context) ([]domain.Instance, error) {
	return f.instances, nil
}
func (f *fakeSource) RuntimeStatus(_ context.Context, num int) (ports.Status, error) {
	return f.status[num], nil
}
func (f *fakeSource) CrashLogs(_ context.Context, num int, _ int64) (io.ReadCloser, error) {
	f.logCalls++
	return io.NopCloser(strings.NewReader(f.logs[num])), nil
}

type fakeRecorder struct {
	last      map[int]*domain.Incident
	recorded  []domain.Incident
	eventKind []string
}

func (r *fakeRecorder) LastIncident(_ context.Context, _ domain.GameID, num int) (*domain.Incident, error) {
	return r.last[num], nil
}
func (r *fakeRecorder) RecordIncident(_ context.Context, in domain.Incident) (int64, error) {
	r.recorded = append(r.recorded, in)
	r.last[in.Number] = &in
	return int64(len(r.recorded)), nil
}
func (r *fakeRecorder) RecordEvent(kind, _ string) error {
	r.eventKind = append(r.eventKind, kind)
	return nil
}

func newRec() *fakeRecorder { return &fakeRecorder{last: map[int]*domain.Incident{}} }

func TestHealthyInstanceRecordsNothing(t *testing.T) {
	src := &fakeSource{
		game:      domain.GameMinecraft,
		instances: []domain.Instance{{Number: 1, Name: "ayyy"}},
		status:    map[int]ports.Status{1: {Lifecycle: ports.LifecycleRunning, Available: true}},
	}
	rec := newRec()

	if n := New(rec, []Source{src}).Check(context.Background()); n != 0 {
		t.Errorf("healthy instance produced %d incidents", n)
	}
	if src.logCalls != 0 {
		t.Error("must not fetch crash logs for a healthy instance")
	}
}

func TestCrashLoopIsRecordedWithLogTail(t *testing.T) {
	src := &fakeSource{
		game:      domain.GameMinecraft,
		instances: []domain.Instance{{Number: 3, Name: "bob"}},
		status: map[int]ports.Status{3: {
			Lifecycle: ports.LifecycleRunning,
			Failure: ports.Failure{
				RestartCount: 10, WaitingReason: "CrashLoopBackOff",
				ExitCode: 1, FinishedAt: time.Now(),
			},
		}},
		logs: map[int]string{3: "Caused by: java.lang.NoClassDefFoundError\n"},
	}
	rec := newRec()

	if n := New(rec, []Source{src}).Check(context.Background()); n != 1 {
		t.Fatalf("expected 1 incident, got %d", n)
	}
	in := rec.recorded[0]
	if in.Reason != "CrashLoopBackOff" || in.RestartCount != 10 {
		t.Errorf("failure detail lost: %+v", in)
	}
	if !strings.Contains(in.LogTail, "NoClassDefFoundError") {
		t.Error("log tail must be captured at detection: kubernetes reaps it when the pod is replaced")
	}
	if len(rec.eventKind) != 1 || rec.eventKind[0] != "crash" {
		t.Errorf("must emit a crash event for the history timeline, got %v", rec.eventKind)
	}
}

func TestSameFailureIsNotRecordedTwice(t *testing.T) {
	fin := time.Now()
	src := &fakeSource{
		game:      domain.GameValheim,
		instances: []domain.Instance{{Number: 2, Name: "boppo"}},
		status: map[int]ports.Status{2: {
			Failure: ports.Failure{RestartCount: 4, ExitCode: 137, FinishedAt: fin, OOMKilled: true},
		}},
	}
	rec := newRec()
	w := New(rec, []Source{src})

	if n := w.Check(context.Background()); n != 1 {
		t.Fatalf("first check should record, got %d", n)
	}
	if n := w.Check(context.Background()); n != 0 {
		t.Errorf("unchanged failure recorded again: the watcher would spam one incident per tick")
	}

	src.status[2] = ports.Status{Failure: ports.Failure{RestartCount: 5, ExitCode: 137, FinishedAt: fin}}
	if n := w.Check(context.Background()); n != 1 {
		t.Errorf("a further restart is a new failure and must be recorded, got %d", n)
	}
}
