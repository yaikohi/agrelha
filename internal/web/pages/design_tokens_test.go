package pages

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var migratedTemplates = []string{
	"dashboard.templ",
	"nav_hearth.templ",
	"panel.templ",
}

var (
	tailwindPalette = regexp.MustCompile(`\b(?:slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-(?:50|[1-9]00|950)\b`)
	rawHex          = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)
	themeToken      = regexp.MustCompile(`(?m)^\s*(--(?:color|font|radius)-[a-z0-9-]+):`)
	docToken        = regexp.MustCompile("`(--(?:color|font|radius)-[a-z0-9-]+)`")
)

func TestMigratedTemplatesNameRolesNotColours(t *testing.T) {
	for _, name := range migratedTemplates {
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
