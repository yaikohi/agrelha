package docker

import (
	"context"
	"testing"

	"agrelha/internal/ports"
)

func restartingInspect() ContainerInspect {
	var in ContainerInspect
	in.State.Status = "restarting"
	in.State.Running = true
	in.State.Restarting = true
	in.State.ExitCode = 1
	in.RestartCount = 33
	return in
}

// Docker reports State.Running true for the whole of a restart loop, so a
// container that has never once started looked identical to a healthy server.
func TestRestartingContainerIsNotAvailable(t *testing.T) {
	r := New(WithClient(&mockClient{inspect: restartingInspect()}))

	st, err := r.Status(context.Background(), ports.ServerRef{Name: "mc-vp-01"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Available {
		t.Error("players cannot connect to a container the restart policy is cycling")
	}
	if !st.Failure.CrashLooping() {
		t.Errorf("expected a crash loop, got %+v", st.Failure)
	}
	if st.Lifecycle != ports.LifecycleRunning {
		t.Errorf("Lifecycle is what the operator asked for and must stay running, got %q", st.Lifecycle)
	}
}

func TestHealthyContainerIsStillAvailable(t *testing.T) {
	var in ContainerInspect
	in.State.Status = "running"
	in.State.Running = true

	r := New(WithClient(&mockClient{inspect: in}))
	st, err := r.Status(context.Background(), ports.ServerRef{Name: "mc-vp-01"})
	if err != nil {
		t.Fatal(err)
	}
	if !st.Available {
		t.Error("a running container with no healthcheck is available")
	}
	if st.Failure.CrashLooping() {
		t.Error("a container that has never restarted is not crash looping")
	}
}
