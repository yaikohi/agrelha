package web

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var convergenceCopyExceptions = map[string]string{
	"shared/convergence.go":          "the helper that owns this wording",
	"pages/mc_provisioning.templ":    "deferred: an ArgoCD-shaped progress flow whose steps, not just its words, are wrong on a Docker Host",
	"pages/mc_provisioning_templ.go": "generated from the above",
}

func TestConvergenceCopyLivesInOnePlace(t *testing.T) {
	var offenders []string

	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".go" && ext != ".templ" {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel := filepath.ToSlash(path)
		if _, ok := convergenceCopyExceptions[rel]; ok {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(body), "ArgoCD") {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if len(offenders) > 0 {
		t.Errorf(`%v names ArgoCD directly.

Whether a change converges later or has already happened depends on the
Instance's Host: ArgoCD syncs a cluster world asynchronously, Compose converges
a Docker Host world before the response is written. Hardcoding one of them tells
half the worlds something untrue.

Use shared.ConvergenceClause(host, unreachable) or shared.CreationSentence(...),
or add a reviewed entry to convergenceCopyExceptions in this file.`, offenders)
	}
}
