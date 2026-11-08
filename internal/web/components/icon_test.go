package components

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func renderToString(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func vendoredSVGs(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	entries, err := fs.ReadDir(iconFiles, "icons")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".svg") {
			continue
		}
		body, err := fs.ReadFile(iconFiles, "icons/"+e.Name())
		if err != nil {
			t.Fatal(err)
		}
		out[strings.TrimSuffix(e.Name(), ".svg")] = string(body)
	}
	if len(out) == 0 {
		t.Fatal("no glyphs vendored; run task assets:icons")
	}
	return out
}

func TestEveryGlyphInheritsTheInkRamp(t *testing.T) {
	for name, body := range vendoredSVGs(t) {
		if !strings.Contains(body, `fill="currentColor"`) {
			t.Errorf(`%s.svg does not set fill="currentColor", so it cannot inherit a role colour.

Icons are monochrome and take their colour from the text around them. A glyph
with a baked-in fill is the same defect design_tokens_test.go catches in markup.`, name)
		}
	}
}

func TestEveryGlyphIsOnTheGrid(t *testing.T) {
	fractional := regexp.MustCompile(`-?\d+\.\d+`)
	for name, body := range vendoredSVGs(t) {
		if !strings.Contains(body, `viewBox="0 0 24 24"`) {
			t.Errorf("%s.svg is not drawn on the 24-unit grid every other glyph uses", name)
		}
		if hits := fractional.FindAllString(body, -1); len(hits) > 0 {
			t.Errorf(`%s.svg has off-grid coordinates: %s

Every coordinate must be a whole grid unit. A fractional one antialiases at
every size and is invisible in review.`, name, strings.Join(hits, ", "))
		}
	}
}

func TestTheVendoredSetAndTheSetInUseAgree(t *testing.T) {
	// Two ways a glyph is asked for: rendered in a template, or named by a view
	// model the templates render.
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?:components\.)?(?:Icon|Empty|SectionHead)\("([a-z0-9-]+)"`),
		regexp.MustCompile(`Icon:\s*"([a-z0-9-]+)"`),
	}
	used := map[string]bool{}
	for _, dir := range []string{".", filepath.Join("..", "pages")} {
		for _, pattern := range []string{"*.templ", "*.go"} {
			files, err := filepath.Glob(filepath.Join(dir, pattern))
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range files {
				if strings.HasSuffix(f, "_templ.go") || strings.HasSuffix(f, "_test.go") {
					continue
				}
				body, err := os.ReadFile(f)
				if err != nil {
					t.Fatal(err)
				}
				for _, re := range patterns {
					for _, m := range re.FindAllStringSubmatch(string(body), -1) {
						used[m[1]] = true
					}
				}
			}
		}
	}

	vendored := vendoredSVGs(t)
	for name := range used {
		if _, ok := vendored[name]; !ok {
			t.Errorf("a template asks for icon %q, which is not vendored — add it to ICONS in the Taskfile and run task assets:icons", name)
		}
	}
	for name := range vendored {
		if !used[name] {
			t.Errorf(`%s.svg is vendored but nothing renders it.

The set is kept to what the product uses: drop it from ICONS, or use it.`, name)
		}
	}
}

func TestTheLicenceTravelsWithTheGlyphs(t *testing.T) {
	body, err := fs.ReadFile(iconFiles, "icons/LICENSE")
	if err != nil {
		t.Fatalf("the Pixelarticons licence is not vendored beside the glyphs: %v", err)
	}
	for _, want := range []string{"MIT License", "Gerrit Halfmann"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the vendored licence does not name %q", want)
		}
	}
}

func TestIconRendersTheGlyphWithoutItsOwnSize(t *testing.T) {
	html := renderToString(t, Icon("play", IconStandard))
	if !strings.Contains(html, "h-6 w-6") {
		t.Errorf("the size class is missing: %s", html)
	}
	if strings.Contains(html, `width="24"`) || strings.Contains(html, `height="24"`) {
		t.Errorf(`the vendored width and height survived, and they win over the size class: %s`, html)
	}
	if !strings.Contains(html, `aria-hidden="true"`) {
		t.Errorf("an icon is never the only signal, so it must be aria-hidden: %s", html)
	}
}
