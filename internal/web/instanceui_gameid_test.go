package web

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var instanceUILiteral = regexp.MustCompile(`(?s)pages\.InstanceUI\{(.*?)\n\t*\}`)

func TestEveryInstanceUILiteralSetsGameID(t *testing.T) {
	var offenders []string

	err := filepath.WalkDir("handlers", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range instanceUILiteral.FindAllStringSubmatch(string(body), -1) {
			if !strings.Contains(m[1], "GameID:") {
				offenders = append(offenders, filepath.ToSlash(path))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if len(offenders) > 0 {
		t.Errorf(`%v builds a pages.InstanceUI without GameID.

GamePath() resolves the game profile from GameID and returns "" when it cannot.
Every per-game URL the template builds then collapses to /api//<num>/..., which
matches no route, returns 200 in 0ms, and makes the button look dead. Nothing
fails loudly, so set GameID on every InstanceUI a handler renders.`, offenders)
	}
}
