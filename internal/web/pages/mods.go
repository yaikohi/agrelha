package pages

import (
	"fmt"
	"strings"
)

type ModResultUI struct {
	Title       string
	Slug        string
	Author      string
	Version     string
	IconURL     string
	Description string
	Downloads   string
	Provider    string
	Installed   bool
	Restricted  bool
	Detailable  bool
}

type ModResultsUI struct {
	Game    string
	Num     int
	Heading string
	Note    string
	Results []ModResultUI
	Query   string
	NextN   int
}

type InstalledModUI struct {
	Label   string
	Slug    string
	Version string
}

type InstalledModsUI struct {
	Game     string
	Num      int
	Mods     []InstalledModUI
	Empty    string
	ReadOnly bool
}

func modPayload(slug, provider string) string {
	if provider == "" {
		return fmt.Sprintf("{payload: {slug: '%s'}}", EscapeJS(slug))
	}
	return fmt.Sprintf("{payload: {slug: '%s', provider: '%s'}}", EscapeJS(slug), EscapeJS(provider))
}

func (r ModResultUI) InstallScript(game string, instanceNumber int) string {
	return fmt.Sprintf("@post('/api/%s/%d/mods/install', %s)", game, instanceNumber, modPayload(r.Slug, r.Provider))
}

func (r ModResultUI) RemoveScript(game string, instanceNumber int) string {
	return fmt.Sprintf("@post('/api/%s/%d/mods/remove', %s)", game, instanceNumber, modPayload(r.Slug, r.Provider))
}

func (r ModResultUI) DetailScript(game string, instanceNumber int) string {
	return fmt.Sprintf("@post('/api/%s/%d/mods/detail', %s); $showModDetail = true", game, instanceNumber, modPayload(r.Slug, ""))
}

func (r ModResultUI) InstalledExpr(game string, instanceNumber int) string {
	pref := fmt.Sprintf("inst_%s_%d", game, instanceNumber)
	return fmt.Sprintf("($%s_mods ? !!$%s_mods['%s'] : %t)", pref, pref, EscapeJS(r.Slug), r.Installed)
}

func (r ModResultUI) Meta() []string {
	var out []string
	if r.Author != "" {
		out = append(out, r.Author)
	}
	if r.Version != "" {
		out = append(out, "v"+r.Version)
	}
	if r.Downloads != "" {
		out = append(out, r.Downloads+" downloads")
	}
	if r.Provider != "" {
		out = append(out, r.Provider)
	}
	return out
}

func (m ModResultsUI) MoreScript() string {
	return fmt.Sprintf("@post('/api/%s/%d/mods/search', {payload: {q: '%s', limit: %d}})",
		m.Game, m.Num, EscapeJS(m.Query), m.NextN)
}

func (i InstalledModUI) RemoveScript(game string, instanceNumber int) string {
	return fmt.Sprintf(
		"if (confirm('Remove %s from this world? It is uninstalled on the next restart.')) @post('/api/%s/%d/mods/remove', {payload: {slug: '%s'}})",
		EscapeJS(i.Label), game, instanceNumber, EscapeJS(i.Slug),
	)
}

func (i InstalledModUI) FilterExpr() string {
	return fmt.Sprintf("$modFilter === '' || '%s'.includes($modFilter.toLowerCase())",
		EscapeJS(strings.ToLower(i.Label)))
}

func ValheimInstalledMods(d InstanceDetailUI) InstalledModsUI {
	ui := InstalledModsUI{
		Game:  "valheim",
		Num:   d.Number,
		Empty: "No mods installed. This world runs clean BepInEx.",
	}
	ui.ReadOnly = !d.CanOperate
	for _, slug := range d.InstalledMods {
		ui.Mods = append(ui.Mods, InstalledModUI{Label: slug, Slug: slug})
	}
	return ui
}

func MinecraftInstalledMods(d InstanceDetailUI) InstalledModsUI {
	empty := "No mods installed in this world."
	if d.Pack != "" {
		empty = fmt.Sprintf("Mods for %s are downloaded on launch from its pack, so none are listed here.", d.Pack)
	}
	ui := InstalledModsUI{Game: "minecraft", Num: d.Number, Empty: empty}
	ui.ReadOnly = !d.CanOperate
	for _, slug := range d.InstalledMods {
		ui.Mods = append(ui.Mods, InstalledModUI{Label: slug, Slug: slug})
	}
	return ui
}
