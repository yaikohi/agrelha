package pages

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func migratedTemplates(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, dir := range []string{".", filepath.Join("..", "components")} {
		found, err := filepath.Glob(filepath.Join(dir, "*.templ"))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, found...)
	}
	if len(out) == 0 {
		t.Fatal("no templates found; has the layout changed?")
	}
	return out
}

var (
	tailwindPalette = regexp.MustCompile(`\b(?:slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-(?:50|[1-9]00|950)\b`)
	rawHex          = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)
	themeToken      = regexp.MustCompile(`(?m)^\s*(--(?:color|font|radius)-[a-z0-9-]+):`)
	docToken        = regexp.MustCompile("`(--(?:color|font|radius)-[a-z0-9-]+)`")
	docCell         = regexp.MustCompile("`([^`]+)`")
	themeTokenValue = regexp.MustCompile(`(?m)^\s*(--(?:color|radius)-[a-z0-9-]+):\s*([^;]+);`)
)

func TestMigratedTemplatesNameRolesNotColours(t *testing.T) {
	for _, name := range migratedTemplates(t) {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if hits := tailwindPalette.FindAllString(string(body), -1); len(hits) > 0 {
				t.Errorf(`%s uses the Tailwind default palette: %s

A migrated template names the role it wants (text-ink-muted), never a colour.
See docs/design-system.md.`, name, strings.Join(hits, ", "))
			}
			if hits := rawHex.FindAllString(string(body), -1); len(hits) > 0 {
				t.Errorf(`%s hardcodes a colour: %s

Add a token to the @theme block in internal/web/assets/css/input.css instead —
a hex spelled in markup is invisible to the next palette change.`, name, strings.Join(hits, ", "))
			}
		})
	}
}

func TestEveryThemeTokenIsDocumented(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("..", "assets", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "design-system.md"))
	if err != nil {
		t.Fatal(err)
	}

	defined := map[string]bool{}
	for _, m := range themeToken.FindAllStringSubmatch(string(css), -1) {
		defined[m[1]] = true
	}
	documented := map[string]bool{}
	for _, line := range strings.Split(string(doc), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		for _, m := range docToken.FindAllStringSubmatch(line, -1) {
			documented[m[1]] = true
		}
	}

	if len(defined) == 0 {
		t.Fatal("no @theme tokens found in input.css; the design system has no definitions")
	}

	for _, tok := range sortedKeys(defined) {
		if !documented[tok] {
			t.Errorf(`%s is defined in input.css but has no row in docs/design-system.md.

A token nobody can look up gets replaced by a hardcoded colour.`, tok)
		}
	}
	for _, tok := range sortedKeys(documented) {
		if !defined[tok] {
			t.Errorf("%s has a row in docs/design-system.md but no longer exists in input.css", tok)
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Some handlers build HTML as Go strings because they are patched in over SSE
// in fragments. design_tokens_test originally globbed only .templ, so those
// files drifted for an entire palette change without anything noticing. These
// are the ones still carrying the pre-hearth palette.
//
// The list may only shrink: a file on it that has been migrated also fails, so
// finishing the job is what removes the entry.
var notYetMigrated = []string{
	"handlers/console/gmod.go",
	"handlers/console/minecraft.go",
	"handlers/console/valheim.go",
	"handlers/valheim/detail.go",
	"handlers/valheim/wizard.go",
	"handlers/wizard/wizard.go",
	"pages/helpers.go",
}

func handwrittenGo(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "assets" {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_templ.go") ||
			strings.HasSuffix(name, "_test.go") {
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("scanned no Go files under internal/web; has the layout changed?")
	}
	sort.Strings(out)
	return out
}

func TestHandlerMarkupNamesRolesNotColours(t *testing.T) {
	pending := map[string]bool{}
	for _, f := range notYetMigrated {
		pending[filepath.FromSlash(f)] = true
	}
	seen := map[string]bool{}

	for _, path := range handwrittenGo(t) {
		rel := strings.TrimPrefix(path, ".."+string(filepath.Separator))
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		hits := append(tailwindPalette.FindAllString(string(body), -1),
			rawHex.FindAllString(string(body), -1)...)

		if pending[rel] {
			seen[rel] = true
			if len(hits) == 0 {
				t.Errorf(`%s is listed in notYetMigrated but no longer uses off-palette colours.

Delete its line from the list so the next file cannot slip back in behind it.`, rel)
			}
			continue
		}
		if len(hits) > 0 {
			t.Errorf(`%s builds markup with a colour instead of a token: %s

HTML assembled in Go is held to the same rule as a template: name the role
(text-ink-muted), never the colour. See docs/design-system.md.`, rel, strings.Join(hits, ", "))
		}
	}

	for _, f := range notYetMigrated {
		if rel := filepath.FromSlash(f); !seen[rel] {
			t.Errorf("notYetMigrated lists %s, which no longer exists under internal/web", f)
		}
	}
}

func TestDocumentedTokenValuesMatchTheStylesheet(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("..", "assets", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "design-system.md"))
	if err != nil {
		t.Fatal(err)
	}

	defined := map[string]string{}
	for _, m := range themeTokenValue.FindAllStringSubmatch(string(css), -1) {
		defined[m[1]] = strings.ToLower(strings.TrimSpace(m[2]))
	}

	for _, line := range strings.Split(string(doc), "\n") {
		cells := strings.Split(line, "|")
		if len(cells) < 4 {
			continue
		}
		name := docToken.FindStringSubmatch(cells[1])
		if name == nil {
			continue
		}
		value := docCell.FindStringSubmatch(cells[2])
		if value == nil {
			continue
		}
		if got, want := defined[name[1]], strings.ToLower(value[1]); got != want {
			t.Errorf(`docs/design-system.md says %s is %s, but input.css defines %s.

A table nobody can trust is worse than no table: fix whichever is stale.`,
				name[1], want, got)
		}
	}
}
