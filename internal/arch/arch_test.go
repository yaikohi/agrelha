package arch

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const module = "agrelha"

var layers = []struct{ prefix, layer string }{
	{"internal/arch", "meta"},
	{"internal/domain", "domain"},
	{"internal/ports", "ports"},
	{"internal/platform", "platform"},
	{"internal/app", "app"},
	{"internal/infra", "infra"},
	{"internal/web", "web"},
	{"internal/server", "web"},
	{"internal/wiring", "root"},
	{"cmd/", "root"},
}

var allowed = map[string]map[string]bool{
	"domain":   {},
	"platform": {},
	"meta":     {},
	"ports":    {"domain": true},
	"app":      {"domain": true, "ports": true, "platform": true, "app": true},
	"infra":    {"domain": true, "ports": true, "platform": true, "infra": true},
	"web":      {"domain": true, "ports": true, "platform": true, "app": true, "web": true},
	"root":     {"domain": true, "ports": true, "platform": true, "app": true, "infra": true, "web": true, "root": true},
}

const configPkg = module + "/internal/platform/config"

var configImporters = map[string]bool{"root": true}

type exception struct {
	importer string
	target   string
	phase    string
}

var exceptions = []exception{
	{"internal/app/backups", "infra", "E"},
	{"internal/app/backups", "config", "I"},
	{"internal/app/content", "infra", "F"},
	{"internal/app/modpack", "infra", "F"},
	{"internal/server", "infra", "E"},
	{"internal/server", "config", "E"},
	{"internal/infra/auth/oidc", "config", "I"},
	{"internal/web/handlers/access", "infra", "E"},
	{"internal/web/handlers/access", "config", "I"},
	{"internal/web/handlers/backups", "infra", "E"},
	{"internal/web/handlers/console", "infra", "E"},
	{"internal/web/handlers/content", "infra", "E"},
	{"internal/web/handlers/content", "config", "I"},
	{"internal/web/handlers/dashboard", "infra", "E"},
	{"internal/web/handlers/dashboard", "config", "I"},
	{"internal/web/handlers/instances", "infra", "E"},
	{"internal/web/handlers/instances", "config", "I"},
	{"internal/web/handlers/wizard", "infra", "E"},
	{"internal/web/pages", "infra", "H"},
}

func layerFor(pkg string) (layer, prefix string) {
	for _, l := range layers {
		if pkg == strings.TrimSuffix(l.prefix, "/") || strings.HasPrefix(pkg, l.prefix) {
			return l.layer, l.prefix
		}
	}
	return "", ""
}

func TestArchitecture(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	imports, err := scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(imports) == 0 {
		t.Fatal("scanned no packages; is the module root ../.. ?")
	}

	found := map[string]bool{}
	seen := map[string]bool{}
	matched := map[string]bool{}
	var unclassified, breaches []string

	for pkg, targets := range imports {
		from, prefix := layerFor(pkg)
		if from == "" {
			unclassified = append(unclassified, pkg)
			continue
		}
		matched[prefix] = true
		for _, target := range targets {
			tp := strings.TrimPrefix(target, module+"/")
			to, _ := layerFor(tp)
			if to == "" {
				continue
			}
			kind := ""
			switch {
			case target == configPkg && !configImporters[from]:
				kind = "config"
			case !allowed[from][to]:
				kind = to
			default:
				continue
			}
			found[pkg+" -> "+kind] = true
			msg := fmt.Sprintf("%s imports %s (%s may not depend on %s)", pkg, tp, from, kind)
			if !excepted(pkg, kind) && !seen[msg] {
				seen[msg] = true
				breaches = append(breaches, msg)
			}
		}
	}

	sort.Strings(unclassified)
	for _, pkg := range unclassified {
		t.Errorf("package %s belongs to no layer; add it to layers or move it", pkg)
	}

	sort.Strings(breaches)
	for _, b := range breaches {
		t.Errorf("forbidden dependency: %s", b)
	}

	for _, e := range exceptions {
		if !found[e.importer+" -> "+e.target] {
			t.Errorf("stale exception %q -> %q (phase %s): the violation is gone, delete the entry", e.importer, e.target, e.phase)
		}
	}

	for _, l := range layers {
		if l.layer == "meta" {
			continue
		}
		if !matched[l.prefix] {
			t.Errorf("stale layer entry %q: no package lives there any more, delete it", l.prefix)
		}
	}

	if len(exceptions) > 0 {
		t.Logf("%d known violations remain; the cleanup is done when this list is empty", len(exceptions))
	}
}

func excepted(pkg, kind string) bool {
	for _, e := range exceptions {
		if e.importer == pkg && e.target == kind {
			return true
		}
	}
	return false
}

func scan(root string) (map[string][]string, error) {
	out := map[string][]string{}
	skip := map[string]bool{".git": true, "node_modules": true, "tmp": true, ".task": true, "deploy": true, "docs": true, "third_party": true}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}

		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		for _, spec := range f.Imports {
			p := strings.Trim(spec.Path.Value, `"`)
			if p == module || strings.HasPrefix(p, module+"/") {
				out[rel] = append(out[rel], p)
			}
		}
		if _, ok := out[rel]; !ok {
			out[rel] = nil
		}
		return nil
	})
	return out, err
}
