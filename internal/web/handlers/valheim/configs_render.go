package valheim

import (
	"fmt"
	"html"
	"strings"

	appbepinex "agrelha/internal/app/bepinex"
	"agrelha/internal/domain"
	"agrelha/internal/web/components"
)

// These fragments are assembled as strings rather than templ because the panel
// is patched in over SSE in pieces. They still speak hearth: every colour is a
// token, and the shared control classes come from components so there is one
// definition of what an input looks like.
const (
	bandClass     = "-mx-6 mt-6 border-t border-seam"
	headClass     = "flex flex-wrap items-start justify-between gap-x-6 gap-y-3 border-b border-seam px-6 py-4"
	metaClass     = "mt-1 flex flex-wrap gap-x-3 font-pixel text-[11px] text-ink-faint"
	proseClass    = "max-w-prose font-display text-sm leading-relaxed text-ink-muted"
	checkboxClass = "focus-accent check"
	quietBtnClass = "focus-accent font-pixel text-[11px] text-ink-faint transition-colors hover:text-accent"
)

func renderPanel(num int, name string, view appbepinex.FileView) string {
	var b strings.Builder
	esc := html.EscapeString

	title := name
	if view.Config.PluginName != "" {
		title = view.Config.PluginName
		if view.Config.PluginVersion != "" {
			title += " v" + view.Config.PluginVersion
		}
	}

	fmt.Fprintf(&b, `<div class="%s" data-signals="%s">`,
		bandClass,
		esc(fmt.Sprintf(`{cfgFile: '%s', cfgSha: '%s', cfgSearch: '', cfgSection: '', cfgOnlyChanged: false, cfgRaw: %t, cfgDirty: {}, cfgForget: []}`,
			jsQuote(name), jsQuote(view.SHA256), !view.Config.Parsable())))

	fmt.Fprintf(&b, `<div class="%s">
		<div class="min-w-0">
			<h3 class="font-display text-base font-semibold text-ink">%s</h3>
			<p class="%s"><span class="break-all">%s</span><span>%d settings</span><span>%s</span></p>
		</div>
		<button type="button" data-on:click="@setAll('cfgFile', ''); document.getElementById('cfg-panel').innerHTML = ''"
			class="%s">Close</button></div>`,
		headClass, esc(title), metaClass, esc(name), view.Config.Count(), humanBytes(view.Size),
		components.ButtonClass(components.VariantSecondary))

	if !view.Config.Parsable() {
		b.WriteString(renderRawEditor(num, name, view))
		b.WriteString(`</div>`)
		return b.String()
	}

	fmt.Fprintf(&b, `<div class="flex flex-wrap items-center gap-3 border-b border-seam px-6 py-3">
		<div class="w-56"><input type="search" data-bind="cfgSearch" placeholder="Search settings"
			data-on:input__debounce.300ms="%s" class="%s"/></div>
		<div class="w-56"><select data-bind="cfgSection" data-on:change="%s" class="%s">
		<option value="">All sections</option>`,
		esc(settingsFetch(num)), components.InputClass(),
		esc(settingsFetch(num)), components.InputClass())
	for _, s := range view.Config.SectionNames() {
		fmt.Fprintf(&b, `<option value="%s">%s</option>`, esc(s), esc(s))
	}
	b.WriteString(`</select></div>`)

	fmt.Fprintf(&b, `<label class="flex items-center gap-2 font-pixel text-[11px] text-ink-muted">
		<input type="checkbox" data-bind="cfgOnlyChanged" data-on:change="%s" class="%s"/>
		Changed from default</label>`, esc(settingsFetch(num)), checkboxClass)

	fmt.Fprintf(&b, `<div class="ml-auto"><button type="button" data-on:click="%s" class="%s">Save changes</button></div></div>`,
		esc(fmt.Sprintf(`@post('/api/valheim/%d/configs/save', {contentType: 'json', payload: {file: $cfgFile, sha256: $cfgSha, values: $cfgDirty, forget: $cfgForget}})`, num)),
		components.ButtonClass(components.VariantPrimary))

	b.WriteString(`<div id="cfg-settings">`)
	managed := managedSettings(view)
	b.WriteString(renderSettings(num, name, view, managed, false, true))
	b.WriteString(`</div>`)
	b.WriteString(`</div>`)
	return b.String()
}

func managedSettings(view appbepinex.FileView) []domain.Setting {
	var out []domain.Setting
	for _, s := range view.Config.Settings() {
		if view.Overrides.Has(s.Section, s.Name) {
			out = append(out, s)
		}
	}
	return out
}

func settingsFetch(num int) string {
	return fmt.Sprintf(
		`@get('/api/valheim/%d/configs/settings?f=' + encodeURIComponent($cfgFile) + '&q=' + encodeURIComponent($cfgSearch) + '&section=' + encodeURIComponent($cfgSection) + '&changed=' + $cfgOnlyChanged)`,
		num)
}

func renderSettings(num int, name string, view appbepinex.FileView, hits []domain.Setting, truncated, onlyManaged bool) string {
	var b strings.Builder

	if len(hits) == 0 {
		if onlyManaged {
			return fmt.Sprintf(`<div class="px-6 py-10"><p class="%s">agrelha is not managing any values in this file yet.
				Search, or pick a section, to see what this mod exposes.</p></div>`, proseClass)
		}
		return fmt.Sprintf(`<div class="px-6 py-10"><p class="%s">No settings match.</p></div>`, proseClass)
	}

	index := settingIndex(view)
	for _, s := range hits {
		i, ok := index[s.Key()]
		if !ok {
			continue
		}
		b.WriteString(renderSetting(num, name, view, s, i))
	}

	if truncated {
		fmt.Fprintf(&b, `<p class="border-b border-seam bg-seam px-6 py-3 font-pixel text-[11px] text-ink-faint">
			Showing the first <span class="text-accent">%d</span> matches. Narrow the search or pick a section to see the rest.</p>`,
			searchLimit)
	}
	return b.String()
}

func settingIndex(view appbepinex.FileView) map[domain.SettingKey]int {
	out := map[domain.SettingKey]int{}
	for i, s := range view.Config.Settings() {
		out[s.Key()] = i
	}
	return out
}

// A managed Setting is marked by a rule down its left edge rather than a badge:
// at two hundred rows a rule is scannable and a badge is noise, and "agrelha
// writes this value" is a property of the row, not a label on it.
func renderSetting(num int, file string, view appbepinex.FileView, s domain.Setting, idx int) string {
	var b strings.Builder
	esc := html.EscapeString
	managed := view.Overrides.Has(s.Section, s.Name)

	rule := "border-l-transparent"
	if managed {
		rule = "border-l-accent"
	}
	fmt.Fprintf(&b, `<div class="border-b border-l-2 border-seam %s px-6 py-4">`, rule)
	fmt.Fprintf(&b, `<div class="flex flex-wrap items-start justify-between gap-x-6 gap-y-3"><div class="min-w-0 flex-1">
		<p class="flex flex-wrap items-baseline gap-x-3">
		<span class="break-all font-pixel text-xs text-ink">%s</span>
		<span class="font-pixel text-[11px] text-ink-faint">%s</span>`,
		esc(s.Name), esc(s.Section))

	if s.DiffersFromDefault() {
		fmt.Fprintf(&b, `<span class="%s" title="differs from the value the mod ships with">changed</span>`,
			components.TagClass(components.TagAccent))
	}
	if managed {
		fmt.Fprintf(&b, `<span class="%s" title="agrelha pins this value in git">managed</span>`,
			components.TagClass(components.TagQuiet))
	}
	if s.Orphaned {
		fmt.Fprintf(&b, `<span class="%s" title="the mod did not describe this setting, so it is edited as free text">undescribed</span>`,
			components.TagClass(components.TagQuiet))
	}
	b.WriteString(`</p>`)

	if s.Description != "" {
		fmt.Fprintf(&b, `<p class="mt-2 %s">%s</p>`, proseClass, esc(truncate(s.Description, 240)))
	}
	meta := []string{}
	if s.HasDefault {
		meta = append(meta, "default: "+s.Default)
	}
	if s.Range != nil {
		meta = append(meta, fmt.Sprintf("%s–%s", s.Range.From, s.Range.To))
	}
	if s.Type != "" {
		meta = append(meta, s.Type)
	}
	if len(meta) > 0 {
		fmt.Fprintf(&b, `<p class="%s">`, metaClass)
		for _, m := range meta {
			fmt.Fprintf(&b, `<span>%s</span>`, esc(m))
		}
		b.WriteString(`</p>`)
	}
	b.WriteString(`</div><div class="flex w-64 shrink-0 flex-col items-end gap-2">`)
	b.WriteString(renderControl(s, idx))

	b.WriteString(`<div class="flex items-center gap-3">`)
	if s.HasDefault {
		fmt.Fprintf(&b, `<button type="button" data-on:click="%s" class="%s"
			title="pins the mod's default as a managed value">Reset to default</button>`,
			esc(fmt.Sprintf(`@post('/api/valheim/%d/configs/reset', {contentType: 'json', payload: {file: '%s', sha256: $cfgSha, section: '%s', setting: '%s'}})`,
				num, jsQuote(file), jsQuote(s.Section), jsQuote(s.Name))),
			quietBtnClass)
	}
	if managed {
		fmt.Fprintf(&b, `<button type="button" data-on:click="%s" class="focus-accent font-pixel text-[11px] text-ink-faint transition-colors hover:text-danger"
			title="stops agrelha managing this value; the server keeps the value it already has">Forget</button>`,
			esc(fmt.Sprintf(`if (confirm('Stop managing %s?\n\nThe server keeps its current value; this only stops agrelha writing it.')) { $cfgForget = [...$cfgForget, '%d'] }`,
				jsQuote(s.Name), idx)))
	}
	b.WriteString(`</div></div></div></div>`)
	return b.String()
}

func renderControl(s domain.Setting, idx int) string {
	esc := html.EscapeString
	base := fmt.Sprintf(`class="%s"`, components.InputClass())

	record := func(event string) string {
		return fmt.Sprintf(`data-on:%s="$cfgDirty['%d'] = evt.target.value"`, event, idx)
	}

	switch s.Control() {
	case domain.ControlToggle:
		checked := ""
		if s.IsOn() {
			checked = " checked"
		}

		return fmt.Sprintf(`<label class="flex items-center gap-2 font-pixel text-[11px] text-ink-muted">
			<input type="checkbox"%s data-on:change="$cfgDirty['%d'] = evt.target.checked ? '%s' : '%s'" class="%s"/>
			<span class="text-ink-faint">%s / %s</span></label>`,
			checked, idx, esc(s.TrueValue()), esc(s.FalseValue()), checkboxClass,
			esc(s.TrueValue()), esc(s.FalseValue()))

	case domain.ControlChoice:
		var b strings.Builder
		fmt.Fprintf(&b, `<select %s %s>`, record("change"), base)
		for _, v := range s.Acceptable {
			sel := ""
			if v == s.Value {
				sel = " selected"
			}
			fmt.Fprintf(&b, `<option value="%s"%s>%s</option>`, esc(v), sel, esc(v))
		}
		b.WriteString(`</select>`)
		return b.String()

	case domain.ControlMulti:
		return fmt.Sprintf(`<input type="text" value="%s" %s %s placeholder="%s"/>`,
			esc(s.Value), record("input"), base, esc(strings.Join(s.Acceptable, ", ")))

	case domain.ControlNumber:
		attrs := ""
		if s.Range != nil {
			attrs = fmt.Sprintf(` min="%s" max="%s"`, esc(s.Range.From), esc(s.Range.To))
		}
		if isWholeNumber(s.Type) {
			attrs += ` step="1"`
		} else {
			attrs += ` step="any"`
		}
		return fmt.Sprintf(`<input type="number"%s value="%s" %s %s/>`, attrs, esc(s.Value), record("input"), base)

	default:
		return fmt.Sprintf(`<input type="text" value="%s" %s %s/>`, esc(s.Value), record("input"), base)
	}
}

func renderRawEditor(num int, name string, view appbepinex.FileView) string {
	esc := html.EscapeString

	return fmt.Sprintf(`<div class="space-y-4 px-6 py-5">
		<p class="%s">
			agrelha could not read this file's structure, so it is edited as text.
			This box holds only the values agrelha manages, in BepInEx syntax, not the mod's own file.
		</p>
		<textarea data-bind="cfgRawContent" rows="14" spellcheck="false" class="%s">%s</textarea>
		<div class="flex justify-end">
		<button type="button" data-on:click="%s" class="%s">Save changes</button></div></div>`,
		proseClass,
		components.TextareaClass(),
		esc(view.Overrides.Encode()),
		esc(fmt.Sprintf(`@post('/api/valheim/%d/configs/raw', {contentType: 'json', payload: {file: '%s', content: $cfgRawContent}})`, num, jsQuote(name))),
		components.ButtonClass(components.VariantPrimary))
}

func isWholeNumber(t string) bool {
	switch strings.ToLower(t) {
	case "int32", "int64", "int16", "byte", "sbyte", "uint32", "uint64", "uint16":
		return true
	}
	return false
}

func jsQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`, "\r", "", "<", `<`, ">", `>`)
	return r.Replace(s)
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func renderImportPanel(num int, files []appbepinex.FileSummary) string {
	esc := html.EscapeString
	preview := fmt.Sprintf(
		`@post('/api/valheim/%d/configs/import/preview', {contentType: 'json', payload: {file: $cfgImportFile, content: $cfgImport}})`, num)
	apply := fmt.Sprintf(
		`@post('/api/valheim/%d/configs/import/apply', {contentType: 'json', payload: {file: $cfgImportFile, sha256: $cfgImportSha, content: $cfgImport}})`, num)

	var picker strings.Builder
	fmt.Fprintf(&picker, `<div class="space-y-1"><label class="block font-pixel text-[11px] text-ink-faint" for="cfg-import-target">Apply to</label>
		<select id="cfg-import-target" data-bind="cfgImportFile" class="%s">
		<option value="">Detect from the file</option>`, components.InputClass())
	for _, f := range files {
		label := f.Name
		if f.PluginName != "" {
			label = f.PluginName + " — " + f.Name
		}
		fmt.Fprintf(&picker, `<option value="%s">%s</option>`, esc(f.Name), esc(label))
	}
	picker.WriteString(`</select></div>`)

	return fmt.Sprintf(`<div class="%s">
		<div class="%s">
			<div class="min-w-0">
				<h3 class="font-display text-base font-semibold text-ink">Import a config file</h3>
				<p class="mt-1 %s">
					Paste a mod's .cfg, or pick the file. agrelha compares it with what the mod generates and stores only the values that differ from its defaults.
					Values you already manage but that the file does not mention are kept.
					r2modman drops the plugin header when it saves, so pick the mod below if it cannot be detected.</p>
			</div>
			<button type="button" data-on:click="$cfgImportOpen = false; $cfgImport = ''" class="%s">Close</button>
		</div>
		<div class="space-y-4 px-6 py-5">
			<input type="file" accept=".cfg,.txt,.ini,.json"
				data-on:change="evt.target.files[0] &amp;&amp; (($cfgImportFile = evt.target.files[0].name), evt.target.files[0].text().then(t =&gt; { $cfgImport = t }))"
				class="focus-accent block w-full font-pixel text-[11px] text-ink-muted file:mr-3 file:rounded-control file:border file:border-edge file:bg-surface-raised file:px-3 file:py-1.5 file:font-pixel file:text-[11px] file:text-ink hover:file:border-accent hover:file:text-accent"/>
			%s
			<textarea data-bind="cfgImport" rows="8" spellcheck="false" placeholder="## Settings file was created by plugin ..." class="%s"></textarea>
			<div class="flex items-center justify-end gap-2">
				<button type="button" data-on:click="%s" class="%s">Check</button>
				<button type="button" data-on:click="%s" class="%s">Import</button>
			</div>
			<div id="cfg-import-preview"></div>
		</div>
	</div>`,
		bandClass, headClass, proseClass,
		components.ButtonClass(components.VariantSecondary),
		picker.String(),
		components.TextareaClass(),
		esc(preview), components.ButtonClass(components.VariantSecondary),
		esc(apply), components.ButtonClass(components.VariantPrimary))
}

func renderImportPreview(num int, name string, view appbepinex.FileView, plan domain.ImportPlan) string {
	var b strings.Builder
	esc := html.EscapeString

	if !plan.Parsed {
		return `<p class="border-l-2 border-danger pl-3 font-pixel text-[11px] text-danger">
			Nothing in that text looks like a BepInEx config. It needs at least one [Section] and one Key = value line.</p>`
	}

	title := name
	if plan.PluginName != "" {
		title = plan.PluginName
		if plan.PluginVersion != "" {
			title += " v" + plan.PluginVersion
		}
		title += " → " + name
	}

	fmt.Fprintf(&b, `<div class="space-y-3 border-t border-seam pt-4" data-signals="%s">`,
		esc(fmt.Sprintf(`{cfgImportFile: '%s', cfgImportSha: '%s'}`, jsQuote(name), jsQuote(view.SHA256))))

	fmt.Fprintf(&b, `<p class="break-all font-pixel text-xs text-ink">%s</p>`, esc(title))

	refused := "text-ink-faint"
	if plan.Count(domain.ImportRejected) > 0 {
		refused = "text-danger"
	}
	fmt.Fprintf(&b, `<p class="flex flex-wrap gap-x-4 font-pixel text-[11px]">
		<span class="text-accent">%d</span> to import
		<span class="text-ink-faint">%d already default</span>
		<span class="text-ink-faint">%d not declared by this mod</span>
		<span class="%s">%d refused</span></p>`,
		plan.Count(domain.ImportApply),
		plan.Count(domain.ImportSame),
		plan.Count(domain.ImportUnmatched),
		refused,
		plan.Count(domain.ImportRejected))

	rows := func(status domain.ImportStatus, label, cls string, showDiff bool) {
		entries := plan.Filter(status)
		if len(entries) == 0 {
			return
		}
		fmt.Fprintf(&b, `<div class="pt-1"><p class="mb-1 font-pixel text-[11px] %s">%s</p>`, cls, esc(label))
		shown := entries
		if len(shown) > 40 {
			shown = shown[:40]
		}
		for _, e := range shown {
			fmt.Fprintf(&b, `<div class="flex items-baseline justify-between gap-3 py-0.5 font-pixel text-[11px]">
				<span class="truncate text-ink-muted">%s <span class="text-ink-faint">[%s]</span></span>`,
				esc(e.Name), esc(e.Section))
			if showDiff {
				fmt.Fprintf(&b, `<span class="shrink-0 text-ink-faint">%s → <span class="text-ink">%s</span></span>`,
					esc(e.Current), esc(e.Value))
			} else {
				fmt.Fprintf(&b, `<span class="shrink-0 text-ink-faint">%s</span>`, esc(truncate(e.Reason, 60)))
			}
			b.WriteString(`</div>`)
		}
		if len(entries) > len(shown) {
			fmt.Fprintf(&b, `<p class="py-0.5 font-pixel text-[11px] text-ink-faint">and %d more</p>`, len(entries)-len(shown))
		}
		b.WriteString(`</div>`)
	}

	rows(domain.ImportApply, "will be imported", "text-ink-muted", true)
	rows(domain.ImportUnmatched, "not declared by this mod, imported anyway", "text-ink-faint", true)
	rows(domain.ImportRejected, "refused", "text-danger", false)

	if !plan.Changes() {
		b.WriteString(`<p class="font-pixel text-[11px] text-ink-faint">Nothing to import: every value matches what the mod already uses.</p>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}
