package ports

import (
	"testing"
	"time"
)

func TestFailureCrashed(t *testing.T) {
	tests := []struct {
		name string
		f    Failure
		want bool
	}{
		{"empty", Failure{}, false},
		{"restart count", Failure{RestartCount: 1}, true},
		{"init restart count", Failure{InitRestartCount: 2}, true},
		{"crash loop", Failure{WaitingReason: "CrashLoopBackOff"}, true},
		{"finished at", Failure{FinishedAt: time.Now()}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.f.Crashed(); got != tt.want {
				t.Errorf("Failure.Crashed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFailureFailedBeforeStart(t *testing.T) {
	tests := []struct {
		name string
		f    Failure
		want bool
	}{
		{"empty", Failure{}, false},
		{"init restart count", Failure{InitRestartCount: 1}, true},
		{"init step", Failure{InitStep: "mod-install"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.f.FailedBeforeStart(); got != tt.want {
				t.Errorf("Failure.FailedBeforeStart() = %v, want %v", got, tt.want)
			}
		})
	}
}
