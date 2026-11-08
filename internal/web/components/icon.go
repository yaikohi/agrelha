package components

import (
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
)

//go:embed icons
var iconFiles embed.FS

// IconSize is the rendered box. Only integer multiples of the 24-unit grid the
// glyphs are drawn on are offered: at 24px one grid unit is one CSS pixel, at
// 48px it is two, and anything between puts a pixel on a fraction of one.
type IconSize string

const (
	IconStandard IconSize = "h-6 w-6"
	IconHero     IconSize = "h-12 w-12"
)

var (
	iconMarkup = map[string]string{}
	iconNames  []string
	sizeAttrs  = regexp.MustCompile(`\s(?:width|height)="\d+"`)
)

func init() {
	entries, err := fs.ReadDir(iconFiles, "icons")
	if err != nil {
		panic(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".svg") {
			continue
		}
		body, err := fs.ReadFile(iconFiles, "icons/"+e.Name())
		if err != nil {
			panic(err)
		}
		name := strings.TrimSuffix(e.Name(), ".svg")
		// The vendored width and height would win over the size class, so the
		// attributes go and the class decides the box.
		iconMarkup[name] = sizeAttrs.ReplaceAllString(strings.TrimSpace(string(body)), "")
		iconNames = append(iconNames, name)
	}
	sort.Strings(iconNames)
}

// IconNames lists the vendored glyphs.
func IconNames() []string { return iconNames }

func iconSVG(name string) string {
	svg, ok := iconMarkup[name]
	if !ok {
		panic(fmt.Sprintf("icon %q is not vendored; run task assets:icons after adding it to ICONS in the Taskfile", name))
	}
	return svg
}
