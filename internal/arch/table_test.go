package arch

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const tableDoc = "docs/architecture.md"

// TestPackageTable keeps the package table in the architecture document honest.
// It compares names only, never prose: a package with no row, or a row naming a
// package that no longer exists. Every layer README this replaced went stale by
// silently missing a package that had been added months earlier, and nothing
// caught it because the compiler has no opinion about documentation.
func TestPackageTable(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	documented, err := tablePackages(filepath.Join(root, tableDoc))
	if err != nil {
		t.Fatalf("read %s: %v", tableDoc, err)
	}
	if len(documented) == 0 {
		t.Fatalf("no package rows found in %s; has the table format changed?", tableDoc)
	}

	actual, err := internalPackages(root)
	if err != nil {
		t.Fatal(err)
	}

	for _, pkg := range missing(actual, documented) {
		t.Errorf("package %s has no row in %s; add one line saying what it is for", pkg, tableDoc)
	}
	for _, pkg := range missing(documented, actual) {
		t.Errorf("%s has a row for %s, which is not a package any more; delete the row", tableDoc, pkg)
	}
}

// tablePackages collects the first cell of every markdown table row whose cell
// is a single backticked path, which is how the package table names a package.
func tablePackages(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) < 2 {
			continue
		}
		name, ok := backticked(strings.TrimSpace(cells[0]))
		if !ok {
			continue
		}
		out[name] = true
	}
	return out, sc.Err()
}

// backticked reports whether s is exactly one `quoted` token, and returns it.
// A cell like "`app` calls `web`" is prose, not a package name, so it is not.
func backticked(s string) (string, bool) {
	if len(s) < 3 || s[0] != '`' || s[len(s)-1] != '`' {
		return "", false
	}
	inner := s[1 : len(s)-1]
	if inner == "" || strings.ContainsAny(inner, "` ") {
		return "", false
	}
	return inner, true
}

// internalPackages lists every directory under internal/ holding Go source,
// named the way the table names them: relative to internal/.
func internalPackages(root string) (map[string]bool, error) {
	base := filepath.Join(root, "internal")
	out := map[string]bool{}
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".go" {
			return err
		}
		rel, err := filepath.Rel(base, filepath.Dir(path))
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = true
		return nil
	})
	return out, err
}

func missing(have, want map[string]bool) []string {
	var out []string
	for k := range have {
		if !want[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
