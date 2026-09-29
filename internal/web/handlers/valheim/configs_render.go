package valheim

import (
	"fmt"
	"html"
	"strings"

	appbepinex "agrelha/internal/app/bepinex"
	"agrelha/internal/domain"
)

// The config fragments are built as HTML strings rather than templ components
// because they are patched in over SSE, which is how the mod-search results in
// detail.go already work.

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

	fmt.Fprintf(&b, `<div class="rounded-xl border border-zinc-800 bg-zinc-900/95 shadow-xl"
		data-signals="%s">`,
		esc(fmt.Sprintf(`{cfgFile: '%s', cfgSha: '%s', cfgSearch: '', cfgSection: '', cfgOnlyChanged: false, cfgRaw: %t, cfgDirty: {}, cfgForget: []}`,
			jsQuote(name), jsQuote(view.SHA256), !view.Config.Parsable())))

	fmt.Fprintf(&b, `<div class="flex items-start justify-between gap-4 border-b border-zinc-800 p-4">
		<div><h3 class="text-sm font-semibold text-zinc-100">%s</h3>
		<div class="mt-0.5 font-mono text-[11px] text-zinc-500">%s · %d settings · %s</div></div>
		<button type="button" data-on:click="@setAll('cfgFile', ''); document.getElementById('cfg-panel').innerHTML = ''"
			class="shrink-0 text-xs text-zinc-400 hover:text-zinc-200">Close</button></div>`,
		esc(title), esc(name), view.Config.Count(), humanBytes(view.Size))

	if !view.Config.Parsable() {
		b.WriteString(renderRawEditor(num, name, view))
		b.WriteString(`</div>`)
		return b.String()
	}

	// Controls. No settings are fetched until one of these is used - see the
	// comment on searchLimit.
	fmt.Fprintf(&b, `<div class="flex flex-wrap items-center gap-2 border-b border-zinc-800 p-4">
		<input type="search" data-bind="cfgSearch" placeholder="Search settings…"
			data-on:input__debounce.300ms="%s"
			class="w-56 rounded-lg border border-zinc-700 bg-zinc-800 px-3 py-1.5 text-xs text-zinc-100 placeholder-zinc-500 focus:border-zinc-400 focus:outline-none"/>
		<select data-bind="cfgSection" data-on:change="%s"
			class="rounded-lg border border-zinc-700 bg-zinc-800 px-3 py-1.5 text-xs text-zinc-100 focus:border-zinc-400 focus:outline-none">
		<option value="">All sections</option>`,
		esc(settingsFetch(num)), esc(settingsFetch(num)))
	for _, s := range view.Config.SectionNames() {
		fmt.Fprintf(&b, `<option value="%s">%s</option>`, esc(s), esc(s))
	}
	b.WriteString(`</select>`)

	fmt.Fprintf(&b, `<label class="flex items-center gap-1.5 text-xs text-zinc-400">
		<input type="checkbox" data-bind="cfgOnlyChanged" data-on:change="%s" class="rounded border-zinc-600 bg-zinc-800"/>
		Changed from default</label>`, esc(settingsFetch(num)))

	fmt.Fprintf(&b, `<div class="ml-auto flex items-center gap-2">
		<button type="button" data-on:click="%s"
			class="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white">Save</button></div></div>`,
		esc(fmt.Sprintf(`@post('/api/valheim/%d/configs/save', {contentType: 'json', payload: {file: $cfgFile, sha256: $cfgSha, values: $cfgDirty, forget: $cfgForget}})`, num)))

	// The initial list is what agrelha already manages: the view an operator
	// wants nearly every time, and a handful of rows rather than a thousand.
	b.WriteString(`<div id="cfg-settings" class="divide-y divide-zinc-800/60">`)
	managed := managedSettings(view)
	b.WriteString(renderSettings(num, name, view, managed, false, true))
	b.WriteString(`</div>`)
	b.WriteString(`</div>`)
	return b.String()
}

// managedSettings lists the Settings the operator has pinned, in file order.
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
	esc := html.EscapeString

	if len(hits) == 0 {
		if onlyManaged {
			return `<div class="p-8 text-center text-xs text-zinc-500">
				agrelha is not managing any values in this file yet.<br/>
				<span class="text-zinc-600">Search, or pick a section, to see what this mod exposes.</span></div>`
		}
		return `<div class="p-8 text-center text-xs text-zinc-500">No settings match.</div>`
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
		fmt.Fprintf(&b, `<div class="bg-amber-950/20 p-3 text-center text-[11px] text-amber-400/90">
			Showing the first %d matches of more. Narrow the search or pick a section.</div>`, searchLimit)
	}
	_ = esc
	return b.String()
}

func settingIndex(view appbepinex.FileView) map[domain.SettingKey]int {
	out := map[domain.SettingKey]int{}
	for i, s := range view.Config.Settings() {
		out[s.Key()] = i
	}
	return out
}

func renderSetting(num int, file string, view appbepinex.FileView, s domain.Setting, idx int) string {
	var b strings.Builder
	esc := html.EscapeString
	managed := view.Overrides.Has(s.Section, s.Name)

	b.WriteString(`<div class="p-4 hover:bg-zinc-800/20">`)
	fmt.Fprintf(&b, `<div class="flex items-start justify-between gap-4"><div class="min-w-0 flex-1">
		<div class="flex flex-wrap items-center gap-2">
		<span class="text-xs font-medium text-zinc-200">%s</span>
		<span class="rounded bg-zinc-800/80 px-1.5 py-0.5 font-mono text-[10px] text-zinc-500">%s</span>`,
		esc(s.Name), esc(s.Section))

	if s.DiffersFromDefault() {
		b.WriteString(`<span class="rounded-full bg-amber-500/10 px-1.5 py-0.5 text-[10px] text-amber-400" title="differs from the value the mod ships with">changed</span>`)
	}
	if managed {
		b.WriteString(`<span class="rounded-full bg-zinc-100/10 px-1.5 py-0.5 text-[10px] text-zinc-300" title="agrelha pins this value in git">managed</span>`)
	}
	if s.Orphaned {
		b.WriteString(`<span class="rounded-full bg-zinc-800 px-1.5 py-0.5 text-[10px] text-zinc-500" title="the mod did not describe this setting, so it is edited as free text">undescribed</span>`)
	}
	b.WriteString(`</div>`)

	if s.Description != "" {
		fmt.Fprintf(&b, `<p class="mt-1 text-[11px] leading-relaxed text-zinc-500">%s</p>`,
			esc(truncate(s.Description, 240)))
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
		fmt.Fprintf(&b, `<div class="mt-1 font-mono text-[10px] text-zinc-600">%s</div>`, esc(strings.Join(meta, " · ")))
	}
	b.WriteString(`</div><div class="flex w-64 shrink-0 flex-col items-end gap-1.5">`)
	b.WriteString(renderControl(s, idx))

	fmt.Fprintf(&b, `<div class="flex items-center gap-2 text-[10px]">`)
	if s.HasDefault {
		fmt.Fprintf(&b, `<button type="button" data-on:click="%s" class="text-zinc-500 hover:text-zinc-300"
			title="pins the mod's default as a managed value">Reset to default</button>`,
			esc(fmt.Sprintf(`@post('/api/valheim/%d/configs/reset', {contentType: 'json', payload: {file: '%s', sha256: $cfgSha, section: '%s', setting: '%s'}})`,
				num, jsQuote(file), jsQuote(s.Section), jsQuote(s.Name))))
	}
	if managed {
		fmt.Fprintf(&b, `<button type="button" data-on:click="%s" class="text-zinc-500 hover:text-amber-400"
			title="stops agrelha managing this value; the server keeps the value it already has">Forget</button>`,
			esc(fmt.Sprintf(`if (confirm('Stop managing %s?\n\nThe server keeps its current value; this only stops agrelha writing it.')) { $cfgForget = [...$cfgForget, '%d'] }`,
				jsQuote(s.Name), idx)))
	}
	b.WriteString(`</div></div></div></div>`)
	return b.String()
}

func renderControl(s domain.Setting, idx int) string {
	esc := html.EscapeString
	base := `class="w-full rounded-lg border border-zinc-700 bg-zinc-800 px-3 py-1.5 text-xs text-zinc-100 focus:border-zinc-400 focus:outline-none"`

	// Every control WRITES its signal on change rather than binding to it.
	// data-bind would create a signal for every rendered setting the moment the
	// fragment loads, so an untouched field would be submitted as "" and read as
	// an edit to empty. cfgDirty must hold only what the operator actually
	// touched, because that is the whole payload.
	record := func(event string) string {
		return fmt.Sprintf(`data-on:%s="$cfgDirty['%d'] = evt.target.value"`, event, idx)
	}

	switch s.Control() {
	case domain.ControlToggle:
		checked := ""
		if s.IsOn() {
			checked = " checked"
		}
		// The on/off words come from the file: a ServerSync Toggle writes On/Off
		// where a plain Boolean writes true/false, and writing the wrong pair
		// gives a value the mod silently ignores.
		return fmt.Sprintf(`<label class="flex items-center gap-2 text-xs text-zinc-300">
			<input type="checkbox"%s data-on:change="$cfgDirty['%d'] = evt.target.checked ? '%s' : '%s'"
				class="h-4 w-4 rounded border-zinc-600 bg-zinc-800"/>
			<span class="font-mono text-[11px] text-zinc-500">%s / %s</span></label>`,
			checked, idx, esc(s.TrueValue()), esc(s.FalseValue()), esc(s.TrueValue()), esc(s.FalseValue()))

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
	// The textarea holds the OVERRIDE SET, never the generated file. Prefilling
	// it with the generated file would put up to 207 KB straight back into a
	// ConfigMap that caps at 1 MiB.
	return fmt.Sprintf(`<div class="space-y-3 p-4">
		<p class="text-[11px] text-zinc-500">
			agrelha could not read this file's structure, so it is edited as text.
			This box holds only the values agrelha manages, in BepInEx syntax — not the mod's own file.
		</p>
		<textarea data-bind="cfgRawContent" rows="14" spellcheck="false"
			class="w-full rounded-xl border border-zinc-700 bg-zinc-950 p-3 font-mono text-xs leading-relaxed text-zinc-200 focus:border-zinc-400 focus:outline-none">%s</textarea>
		<div class="flex justify-end">
		<button type="button" data-on:click="%s"
			class="rounded-lg bg-zinc-100 px-4 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white">Save</button></div></div>`,
		esc(view.Overrides.Encode()),
		esc(fmt.Sprintf(`@post('/api/valheim/%d/configs/raw', {contentType: 'json', payload: {file: '%s', content: $cfgRawContent}})`, num, jsQuote(name))))
}

func isWholeNumber(t string) bool {
	switch strings.ToLower(t) {
	case "int32", "int64", "int16", "byte", "sbyte", "uint32", "uint64", "uint16":
		return true
	}
	return false
}

// jsQuote escapes a value for a single-quoted JavaScript string literal inside
// a Datastar expression attribute. BepInEx section and setting names are
// arbitrary mod-authored text.
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
