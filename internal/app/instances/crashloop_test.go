package instances

import (
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

func TestStateFromStatusTellsACrashLoopFromAHealthyServer(t *testing.T) {
	cases := []struct {
		name string
		st   ports.Status
		want domain.InstanceState
	}{
		{
			name: "a container the restart policy is cycling is not running",
			st: ports.Status{
				Lifecycle: ports.LifecycleRunning,
				Available: false,
				Failure:   ports.Failure{Restarting: true, RestartCount: 33, ExitCode: 1},
			},
			want: domain.StateError,
		},
		{
			name: "a Pod in CrashLoopBackOff is not running",
			st: ports.Status{
				Lifecycle: ports.LifecycleRunning,
				Failure:   ports.Failure{WaitingReason: "CrashLoopBackOff", RestartCount: 7},
			},
			want: domain.StateError,
		},
		{
			name: "a server that crashed once and recovered is running",
			st: ports.Status{
				Lifecycle: ports.LifecycleRunning,
				Available: true,
				Failure:   ports.Failure{RestartCount: 1, ExitCode: 1},
			},
			want: domain.StateRunning,
		},
		{
			name: "a server still installing mods is provisioning, not crashing",
			st: ports.Status{
				Lifecycle: ports.LifecycleRunning,
				Available: false,
			},
			want: domain.StateProvisioning,
		},
		{
			name: "a stopped server stays stopped",
			st:   ports.Status{Lifecycle: ports.LifecycleStopped},
			want: domain.StateStopped,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stateFromStatus(c.st); got != c.want {
				t.Errorf("stateFromStatus = %q, want %q", got, c.want)
			}
		})
	}
}
