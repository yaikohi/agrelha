package logging

import (
	"log/slog"
	"testing"
)

func TestSetup_And_ParseLevel(t *testing.T) {
	orig := slog.Default()
	defer slog.SetDefault(orig)

	tests := []struct {
		level     string
		format    string
		wantLevel slog.Level
	}{
		{level: "debug", format: "json", wantLevel: slog.LevelDebug},
		{level: "warn", format: "text", wantLevel: slog.LevelWarn},
		{level: "warning", format: "text", wantLevel: slog.LevelWarn},
		{level: "error", format: "json", wantLevel: slog.LevelError},
		{level: "info", format: "", wantLevel: slog.LevelInfo},
		{level: "unknown", format: "", wantLevel: slog.LevelInfo},
	}

	for _, tc := range tests {
		t.Run(tc.level+"_"+tc.format, func(t *testing.T) {
			Setup(tc.level, tc.format)
			lvl := parseLevel(tc.level)
			if lvl != tc.wantLevel {
				t.Errorf("parseLevel(%q) = %v, want %v", tc.level, lvl, tc.wantLevel)
			}
		})
	}
}
