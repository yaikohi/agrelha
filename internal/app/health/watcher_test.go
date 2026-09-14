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
	logStep   string
}

func (f *fakeSource) GameID() domain.GameID { return f.game }
func (f *fakeSource) ListInstances(context.Context) ([]domain.Instance, error) {
	return f.instances, nil
}
func (f *fakeSource) RuntimeStatus(_ context.Context, num int) (ports.Status, error) {
	return f.status[num], nil
}
func (f *fakeSource) CrashLogs(_ context.Context, num int, _ int64, step ...string) (io.ReadCloser, error) {
	f.logCalls++
	if len(step) > 0 {
		f.logStep = step[0]
	}
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

func TestInitFailureIsRecordedAsItsOwnKindOfFailure(t *testing.T) {
	src := &fakeSource{
		game:      domain.GameValheim,
		instances: []domain.Instance{{Number: 2, Name: "boppo"}},
		status: map[int]ports.Status{2: {
			Lifecycle: ports.LifecycleRunning,
			Failure: ports.Failure{
				InitRestartCount: 5, InitStep: "mod-reconciler",
				WaitingReason: "CrashLoopBackOff", ExitCode: 1,
			},
		}},
		logs: map[int]string{2: "ERROR: could not resolve latest version of blacks7ar/BowPlugin"},
	}
	rec := newRec()

	if n := New(rec, []Source{src}).Check(context.Background()); n != 1 {
		t.Fatalf("an init crash-loop must be recorded, got %d incidents", n)
	}
	in := rec.recorded[0]
	if in.Step != "mod-reconciler" {
		t.Errorf("the failing step must be recorded: %+v", in)
	}
	if in.RestartCount != 5 {
		t.Errorf("init attempts must be counted, got %d", in.RestartCount)
	}
	if !strings.Contains(in.Summary(), "never started") {
		t.Errorf("summary must not read as a game crash: %q", in.Summary())
	}
	if !strings.Contains(in.LogTail, "BowPlugin") {
		t.Error("the init container's log is where the cause lives")
	}
	if src.logStep != "mod-reconciler" {
		t.Errorf("logs must be read from the failing step, not the server container (got %q) — the server never started, so it has none", src.logStep)
	}
}

type errReader struct{}

func (e *errReader) Read(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}
func (e *errReader) Close() error { return nil }

type failingSource struct {
	fakeSource
	listErr bool
	logsErr bool
	readErr bool
}

func (f *failingSource) ListInstances(context.Context) ([]domain.Instance, error) {
	if f.listErr {
		return nil, context.Canceled
	}
	return f.fakeSource.ListInstances(context.Background())
}

func (f *failingSource) CrashLogs(ctx context.Context, num int, tail int64, step ...string) (io.ReadCloser, error) {
	if f.logsErr {
		return nil, context.Canceled
	}
	if f.readErr {
		return &errReader{}, nil
	}
	return f.fakeSource.CrashLogs(ctx, num, tail, step...)
}

type failingRecorder struct {
	fakeRecorder
	lastErr   bool
	recordErr bool
}

func (r *failingRecorder) LastIncident(ctx context.Context, g domain.GameID, num int) (*domain.Incident, error) {
	if r.lastErr {
		return nil, context.Canceled
	}
	return r.fakeRecorder.LastIncident(ctx, g, num)
}

func (r *failingRecorder) RecordIncident(ctx context.Context, in domain.Incident) (int64, error) {
	if r.recordErr {
		return 0, context.Canceled
	}
	return r.fakeRecorder.RecordIncident(ctx, in)
}

func TestWatcherStartAndOptions(t *testing.T) {
	// 1. Guard checks on nil/empty Start
	var nilWatcher *Watcher
	nilWatcher.Start(context.Background())

	wNoRec := New(nil, nil)
	wNoRec.Start(context.Background())

	// 2. Start with ticker and context cancellation
	src := &fakeSource{
		game:      domain.GameValheim,
		instances: []domain.Instance{{Number: 1, Name: "tick"}},
		status:    map[int]ports.Status{1: {Lifecycle: ports.LifecycleRunning}},
	}
	rec := newRec()
	w := New(rec, []Source{src}, WithInterval(5*time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	w.Start(ctx)
	<-ctx.Done()
}

func TestWatcherErrorBranches(t *testing.T) {
	ctx := context.Background()

	// 1. ListInstances error
	fSrc := &failingSource{listErr: true}
	rec := newRec()
	w := New(rec, []Source{fSrc})
	if n := w.Check(ctx); n != 0 {
		t.Errorf("expected 0 incidents on list error, got %d", n)
	}

	// 2. LastIncident error
	fRec := &failingRecorder{lastErr: true}
	srcCrash := &failingSource{
		fakeSource: fakeSource{
			game:      domain.GameMinecraft,
			instances: []domain.Instance{{Number: 1, Name: "mc1"}},
			status: map[int]ports.Status{
				1: {Failure: ports.Failure{RestartCount: 1, ExitCode: 1}},
			},
		},
	}
	wLastErr := New(fRec, []Source{srcCrash})
	if n := wLastErr.Check(ctx); n != 0 {
		t.Errorf("expected 0 incidents on LastIncident error, got %d", n)
	}

	// 3. RecordIncident error
	fRecRecord := &failingRecorder{recordErr: true}
	wRecordErr := New(fRecRecord, []Source{srcCrash})
	if n := wRecordErr.Check(ctx); n != 0 {
		t.Errorf("expected 0 incidents on RecordIncident error, got %d", n)
	}

	// 4. CrashLogs error in captureTail
	srcCrash.logsErr = true
	wLogsErr := New(newRec(), []Source{srcCrash})
	if n := wLogsErr.Check(ctx); n != 1 {
		t.Errorf("expected 1 incident recorded even if CrashLogs fails, got %d", n)
	}

	// 5. Read error in captureTail
	srcCrash.logsErr = false
	srcCrash.readErr = true
	wReadErr := New(newRec(), []Source{srcCrash})
	if n := wReadErr.Check(ctx); n != 1 {
		t.Errorf("expected 1 incident recorded on read error, got %d", n)
	}

	// 6. isNew when InitStep changes
	lastInc := &domain.Incident{Number: 1, Step: "step-a"}
	isDifferentStep := isNew(ports.Failure{InitStep: "step-b"}, lastInc)
	if !isDifferentStep {
		t.Errorf("expected isNew to be true when InitStep differs")
	}
}

