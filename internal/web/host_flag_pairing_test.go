package web

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostUnreachableAndHostUnknownAreAlwaysSetTogether(t *testing.T) {
	var offenders []string

	err := filepath.WalkDir("handlers", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		unreachable := strings.Count(string(body), "HostUnreachable:")
		unknown := strings.Count(string(body), "HostUnknown:")
		if unreachable != unknown {
			offenders = append(offenders,
				filepath.ToSlash(path)+" sets HostUnreachable "+itoa(unreachable)+"× and HostUnknown "+itoa(unknown)+"×")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if len(offenders) > 0 {
		t.Errorf(`%v

A Host that cannot be reached and a Host no runtime serves are different conditions
with different remedies: wait for the machine, versus fix the configuration. The status
pill derives its label from both flags, so a handler that forwards one and drops the
other renders a misconfigured world as merely unreachable - and the page then tells the
operator to wait for a Host that is never coming back.

Forward both wherever either is set.`, offenders)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
