package domain

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// A BepInEx Generated config is not free-form text. BepInEx's ConfigFile.Save()
// emits every entry the same way - a `##` description, then `# Setting type:`,
// `# Default value:` and optionally the values it will accept, then `Key = value`
// - so a mod's own file tells us enough to build a typed form for it. Nothing
// here talks to a mod or a file: this is the grammar and nothing else.

// SettingRange is the inclusive bound BepInEx writes for a numeric entry
// declared with AcceptableValueRange. From and To are kept as written rather
// than parsed, because the file is the record and re-formatting "3" into "3.0"
// would show the operator a value their mod never wrote.
type SettingRange struct {
	From string
	To   string
}

// ControlKind is how one Setting should be edited. It is decided here and not
// in the templates because "Boolean is a checkbox" is a fact about BepInEx, not
// a fact about HTML.
type ControlKind string

const (
	ControlToggle   ControlKind = "toggle"
	ControlChoice   ControlKind = "choice"
	ControlMulti    ControlKind = "multi"
	ControlNumber   ControlKind = "number"
	ControlShortcut ControlKind = "shortcut"
	ControlText     ControlKind = "text"
)

// Setting is one entry of a Generated config, as the mod described it.
// Everything except Value is documentation the plugin emitted; Value is what
// the file holds right now.
type Setting struct {
	Section        string
	Name           string
	Description    string
	Type           string
	Default        string
	HasDefault     bool
	Acceptable     []string
	Range          *SettingRange
	MultipleValues bool
	Value          string
	// Orphaned marks an entry BepInEx wrote with no description block, which is
	// what it does for a key no live ConfigEntry claims any more. It is also the
	// shape agrelha appends an Override in, and BepInEx preserves both.
	Orphaned bool

	// lineIndex is where this entry's `Key = value` line sits in the original
	// file, so Merge can rewrite that one line and leave every other byte alone.
	lineIndex int
	// separator is the exact text between key and value as written (" = ",
	// "=", ...), preserved so a merge does not reformat the file.
	separator string
}

// Key identifies a Setting within its file.
func (s Setting) Key() SettingKey { return SettingKey{Section: s.Section, Name: s.Name} }

// SettingKey addresses one Setting. Section and Name both carry spaces in real
// files ("[2 - Mining]", "Mining Yield Factor"), so this is never flattened
// into a single delimited string.
type SettingKey struct {
	Section string
	Name    string
}

func (k SettingKey) String() string { return "[" + k.Section + "] " + k.Name }

// Control decides which input edits this Setting.
func (s Setting) Control() ControlKind {
	if s.MultipleValues {
		return ControlMulti
	}
	if strings.EqualFold(s.Type, "Boolean") {
		return ControlToggle
	}
	if len(s.Acceptable) == 2 && isOffOn(s.Acceptable) {
		return ControlToggle
	}
	if len(s.Acceptable) > 0 {
		return ControlChoice
	}
	if isNumericType(s.Type) {
		return ControlNumber
	}
	if strings.EqualFold(s.Type, "KeyboardShortcut") {
		return ControlShortcut
	}
	return ControlText
}

// TrueValue and FalseValue give the on/off words this particular Setting uses.
// Mods disagree - BepInEx's own Boolean writes true/false, while the widespread
// ServerSync "Toggle" type writes On/Off - and writing the wrong pair produces a
// value the mod silently ignores.
func (s Setting) TrueValue() string {
	for _, v := range s.Acceptable {
		if strings.EqualFold(v, "On") {
			return v
		}
	}
	return "true"
}

func (s Setting) FalseValue() string {
	for _, v := range s.Acceptable {
		if strings.EqualFold(v, "Off") {
			return v
		}
	}
	return "false"
}

// IsOn reports whether a toggle Setting currently reads as enabled.
func (s Setting) IsOn() bool {
	v := strings.TrimSpace(s.Value)
	return strings.EqualFold(v, "true") || strings.EqualFold(v, "on")
}

// DiffersFromDefault reports whether the file's value has moved away from what
// the mod shipped. A Setting with no `# Default value:` line cannot answer, and
// says no rather than guessing.
func (s Setting) DiffersFromDefault() bool {
	if !s.HasDefault {
		return false
	}
	return strings.TrimSpace(s.Value) != strings.TrimSpace(s.Default)
}

// Validate checks a candidate value against what the mod said it accepts. It is
// deliberately strict about enums and ranges and permissive about everything
// else: the metadata is the only contract we have, and inventing extra rules
// would reject values the mod is perfectly happy with.
func (s Setting) Validate(v string) error {
	v = strings.TrimSpace(v)
	if len(s.Acceptable) > 0 && !s.MultipleValues {
		for _, a := range s.Acceptable {
			if strings.EqualFold(a, v) {
				return nil
			}
		}
		return fmt.Errorf("%q is not one of: %s", v, strings.Join(s.Acceptable, ", "))
	}
	if s.MultipleValues {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			ok := false
			for _, a := range s.Acceptable {
				if strings.EqualFold(a, part) {
					ok = true
					break
				}
			}
			if !ok {
				return fmt.Errorf("%q is not one of: %s", part, strings.Join(s.Acceptable, ", "))
			}
		}
		return nil
	}
	if strings.EqualFold(s.Type, "Boolean") {
		if !strings.EqualFold(v, "true") && !strings.EqualFold(v, "false") {
			return fmt.Errorf("%q is not a boolean", v)
		}
		return nil
	}
	if isIntegerType(s.Type) {
		if _, err := strconv.ParseInt(v, 10, 64); err != nil {
			return fmt.Errorf("%q is not a whole number", v)
		}
	} else if isFloatType(s.Type) {
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return fmt.Errorf("%q is not a number", v)
		}
	}
	if s.Range != nil {
		got, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("%q is not a number", v)
		}
		lo, errLo := strconv.ParseFloat(s.Range.From, 64)
		hi, errHi := strconv.ParseFloat(s.Range.To, 64)
		if errLo == nil && errHi == nil && (got < lo || got > hi) {
			return fmt.Errorf("%s is outside the accepted range %s to %s", v, s.Range.From, s.Range.To)
		}
	}
	return nil
}

// ConfigSection groups the Settings under one `[Section]` header, in the order
// BepInEx wrote them.
type ConfigSection struct {
	Name     string
	Settings []Setting
}

// ParseWarning records a line the parser did not recognise. Parsing never
// fails: a file we only half-understand still round-trips byte-for-byte, and
// the warnings are what tell the UI to offer the raw editor instead of a form
// it would fill in wrongly.
type ParseWarning struct {
	Line   int
	Text   string
	Reason string
}

// ConfigFile is a parsed Generated config.
type ConfigFile struct {
	Name          string
	HeaderLines   []string
	PluginName    string
	PluginVersion string
	PluginGUID    string
	Sections      []ConfigSection
	Warnings      []ParseWarning

	// lines is the file exactly as it arrived. Render and Merge work on it, so
	// anything the parser did not model still survives a write.
	lines []string
}

const (
	metaSettingType   = "Setting type:"
	metaDefaultValue  = "Default value:"
	metaAcceptable    = "Acceptable values:"
	metaAcceptableRng = "Acceptable value range:"
	metaMultiple      = "Multiple values can be set at the same time"
	headerCreatedBy   = "Settings file was created by plugin "
	headerGUID        = "Plugin GUID: "
)

// ParseConfigFile reads a BepInEx .cfg. It never returns an error: an
// unrecognised file yields Warnings and no Sections, which callers check with
// Parsable before offering a typed editor.
func ParseConfigFile(name, body string) ConfigFile {
	normalised := strings.ReplaceAll(body, "\r\n", "\n")
	f := ConfigFile{Name: name}
	if normalised == "" {
		f.lines = nil
		return f
	}
	f.lines = strings.Split(normalised, "\n")

	var (
		curSection  = -1
		desc        []string
		pending     Setting
		havePending bool
		seenSection bool
	)

	reset := func() {
		desc = nil
		pending = Setting{}
		havePending = false
	}

	for i, raw := range f.lines {
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
			continue

		case strings.HasPrefix(line, "##"):
			text := strings.TrimSpace(strings.TrimPrefix(line, "##"))
			if !seenSection {
				f.HeaderLines = append(f.HeaderLines, raw)
				if rest, ok := cutPrefix(text, headerCreatedBy); ok {
					f.PluginName, f.PluginVersion = splitPluginAndVersion(rest)
				}
				if rest, ok := cutPrefix(text, headerGUID); ok {
					f.PluginGUID = rest
				}
				continue
			}
			desc = append(desc, text)
			havePending = true

		case strings.HasPrefix(line, "#"):
			meta := strings.TrimSpace(strings.TrimPrefix(line, "#"))
			switch {
			case strings.HasPrefix(meta, metaSettingType):
				pending.Type = strings.TrimSpace(strings.TrimPrefix(meta, metaSettingType))
			case strings.HasPrefix(meta, metaDefaultValue):
				pending.Default = strings.TrimSpace(strings.TrimPrefix(meta, metaDefaultValue))
				pending.HasDefault = true
			case strings.HasPrefix(meta, metaAcceptableRng):
				pending.Range = parseRange(strings.TrimSpace(strings.TrimPrefix(meta, metaAcceptableRng)))
			case strings.HasPrefix(meta, metaAcceptable):
				pending.Acceptable = splitAcceptable(strings.TrimSpace(strings.TrimPrefix(meta, metaAcceptable)))
			case strings.HasPrefix(meta, metaMultiple):
				pending.MultipleValues = true
			}
			havePending = true

		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			seenSection = true
			f.Sections = append(f.Sections, ConfigSection{Name: strings.TrimSpace(line[1 : len(line)-1])})
			curSection = len(f.Sections) - 1
			reset()

		case strings.Contains(line, "="):
			key, sep, value := splitEntry(raw)
			if key == "" {
				f.Warnings = append(f.Warnings, ParseWarning{Line: i + 1, Text: raw, Reason: "entry has no key"})
				reset()
				continue
			}
			if curSection < 0 {
				// BepInEx always writes a [Section] before any entry. A bare
				// entry means this is not a file we understand, and guessing a
				// section for it would put the Override somewhere the mod never
				// reads.
				f.Warnings = append(f.Warnings, ParseWarning{Line: i + 1, Text: raw, Reason: "entry before any section header"})
				reset()
				continue
			}
			pending.Section = f.Sections[curSection].Name
			pending.Name = key
			pending.Value = value
			pending.Description = strings.Join(desc, "\n")
			pending.Orphaned = !havePending
			pending.lineIndex = i
			pending.separator = sep
			f.Sections[curSection].Settings = append(f.Sections[curSection].Settings, pending)
			reset()

		default:
			f.Warnings = append(f.Warnings, ParseWarning{Line: i + 1, Text: raw, Reason: "unrecognised line"})
			reset()
		}
	}
	return f
}

// Render writes the file back out. It is the original bytes: the parser keeps
// every line it read, so nothing it failed to understand is lost on a save.
func (f ConfigFile) Render() string {
	if len(f.lines) == 0 {
		return ""
	}
	return strings.Join(f.lines, "\n")
}

// Parsable reports whether this file can be edited as a form. A file with
// warnings, or with no sections at all, gets the raw editor instead - a typed
// form over a file we misread would write values into the wrong places.
func (f ConfigFile) Parsable() bool {
	return len(f.Warnings) == 0 && len(f.Sections) > 0
}

// Settings flattens every Setting in file order.
func (f ConfigFile) Settings() []Setting {
	var out []Setting
	for _, s := range f.Sections {
		out = append(out, s.Settings...)
	}
	return out
}

// Count reports how many Settings the file declares.
func (f ConfigFile) Count() int {
	n := 0
	for _, s := range f.Sections {
		n += len(s.Settings)
	}
	return n
}

// ChangedCount reports how many Settings have moved off the mod's default.
func (f ConfigFile) ChangedCount() int {
	n := 0
	for _, sec := range f.Sections {
		for _, s := range sec.Settings {
			if s.DiffersFromDefault() {
				n++
			}
		}
	}
	return n
}

// Lookup finds one Setting by section and name.
func (f ConfigFile) Lookup(section, name string) (Setting, bool) {
	for _, sec := range f.Sections {
		if sec.Name != section {
			continue
		}
		for _, s := range sec.Settings {
			if s.Name == name {
				return s, true
			}
		}
	}
	return Setting{}, false
}

// SectionNames lists the section headers in file order.
func (f ConfigFile) SectionNames() []string {
	out := make([]string, 0, len(f.Sections))
	for _, s := range f.Sections {
		out = append(out, s.Name)
	}
	return out
}

// SearchQuery narrows a file down to something a page can actually render.
// Therzie.Warfare.cfg is 207 KB and thousands of entries; handing all of them
// to a template is a multi-megabyte response and a hung browser.
type SearchQuery struct {
	Text        string
	Section     string
	OnlyChanged bool
	Limit       int
}

// Search returns the matching Settings and whether the result was cut short.
// Truncation is reported rather than hidden, because a silently clipped list
// reads as "this mod has no such setting".
func (f ConfigFile) Search(q SearchQuery) ([]Setting, bool) {
	needle := strings.ToLower(strings.TrimSpace(q.Text))
	var hits []Setting
	for _, sec := range f.Sections {
		if q.Section != "" && sec.Name != q.Section {
			continue
		}
		for _, s := range sec.Settings {
			if q.OnlyChanged && !s.DiffersFromDefault() {
				continue
			}
			if needle != "" && !settingMatches(s, needle) {
				continue
			}
			if q.Limit > 0 && len(hits) == q.Limit {
				return hits, true
			}
			hits = append(hits, s)
		}
	}
	return hits, false
}

func settingMatches(s Setting, needle string) bool {
	return strings.Contains(strings.ToLower(s.Name), needle) ||
		strings.Contains(strings.ToLower(s.Section), needle) ||
		strings.Contains(strings.ToLower(s.Description), needle)
}

func splitEntry(raw string) (key, sep, value string) {
	i := strings.Index(raw, "=")
	if i < 0 {
		return "", "", ""
	}
	left, right := raw[:i], raw[i+1:]
	key = strings.TrimSpace(left)
	value = strings.TrimSpace(right)

	// Keep the separator exactly as written so a merge does not reflow the file.
	sep = left[len(strings.TrimRight(left, " \t")):] + "=" + right[:len(right)-len(strings.TrimLeft(right, " \t"))]
	return key, sep, value
}

func splitAcceptable(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseRange(s string) *SettingRange {
	rest, ok := cutPrefix(s, "From ")
	if !ok {
		return nil
	}
	from, to, ok := strings.Cut(rest, " to ")
	if !ok {
		return nil
	}
	return &SettingRange{From: strings.TrimSpace(from), To: strings.TrimSpace(to)}
}

// splitPluginAndVersion splits "Some Plugin Name v1.2.3". Plugin names contain
// spaces, so this splits on the last " v" rather than the first.
func splitPluginAndVersion(s string) (name, version string) {
	i := strings.LastIndex(s, " v")
	if i < 0 {
		return strings.TrimSpace(s), ""
	}
	return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+2:])
}

func cutPrefix(s, prefix string) (string, bool) {
	if strings.HasPrefix(s, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(s, prefix)), true
	}
	return s, false
}

func isOffOn(vals []string) bool {
	var off, on bool
	for _, v := range vals {
		if strings.EqualFold(v, "Off") {
			off = true
		}
		if strings.EqualFold(v, "On") {
			on = true
		}
	}
	return off && on
}

func isIntegerType(t string) bool {
	switch strings.ToLower(t) {
	case "int32", "int64", "int16", "byte", "sbyte", "uint32", "uint64", "uint16":
		return true
	}
	return false
}

func isFloatType(t string) bool {
	switch strings.ToLower(t) {
	case "single", "double", "decimal", "float":
		return true
	}
	return false
}

func isNumericType(t string) bool { return isIntegerType(t) || isFloatType(t) }

// Override is one Setting the operator deliberately pinned. Its existence, not
// its value, is the statement: an Override that currently equals the mod's
// default is still kept, because a mod update can move that default and the
// point of recording it was to say "this value, regardless".
type Override struct {
	Section string
	Name    string
	Value   string
}

func (o Override) Key() SettingKey { return SettingKey{Section: o.Section, Name: o.Name} }

// OverrideSet is every Override for one Generated config. This - and only this
// - is what lives in git. The Generated config itself never does: three of
// instance-02's files are over 100 KB against a 1 MiB ConfigMap ceiling, and
// storing them would make git claim to know which Settings a mod has, which it
// cannot.
type OverrideSet struct {
	items map[SettingKey]string
}

func NewOverrideSet() OverrideSet { return OverrideSet{items: map[SettingKey]string{}} }

func (o *OverrideSet) Set(section, name, value string) {
	if o.items == nil {
		o.items = map[SettingKey]string{}
	}
	o.items[SettingKey{Section: section, Name: name}] = value
}

// Unset removes an Override and reports whether there was one. It does not
// restore the mod's default: the Generated config on disk keeps whatever value
// was last written to it, and only a fresh write changes that.
func (o *OverrideSet) Unset(section, name string) bool {
	k := SettingKey{Section: section, Name: name}
	if _, ok := o.items[k]; !ok {
		return false
	}
	delete(o.items, k)
	return true
}

func (o OverrideSet) Get(section, name string) (string, bool) {
	v, ok := o.items[SettingKey{Section: section, Name: name}]
	return v, ok
}

func (o OverrideSet) Has(section, name string) bool {
	_, ok := o.items[SettingKey{Section: section, Name: name}]
	return ok
}

func (o OverrideSet) Len() int { return len(o.items) }

// All lists the Overrides in canonical order.
func (o OverrideSet) All() []Override {
	out := make([]Override, 0, len(o.items))
	for k, v := range o.items {
		out = append(out, Override{Section: k.Section, Name: k.Name, Value: v})
	}
	sortOverrides(out)
	return out
}

func sortOverrides(in []Override) {
	sort.Slice(in, func(i, j int) bool {
		if in[i].Section != in[j].Section {
			return in[i].Section < in[j].Section
		}
		return in[i].Name < in[j].Name
	})
}

// Encode writes the set as a BepInEx-syntax fragment - the exact text stored in
// the Instance's configs ConfigMap. Sections and keys are sorted, so the same
// set always produces the same bytes and git shows a diff only when the
// operator's intent actually changed.
//
// An empty set encodes to "" and the caller deletes the key rather than writing
// an empty one; an empty value would make the merge write an empty file.
func (o OverrideSet) Encode() string {
	if len(o.items) == 0 {
		return ""
	}
	var b strings.Builder
	var cur string
	for i, ov := range o.All() {
		if ov.Section != cur || i == 0 {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString("[" + ov.Section + "]\n")
			cur = ov.Section
		}
		b.WriteString(ov.Name + " = " + ov.Value + "\n")
	}
	return b.String()
}

// ParseOverrideSet reads back what Encode wrote. It accepts any BepInEx
// fragment, so a whole Generated config pasted in is a valid Override set where
// every key is overridden - which is how the one config already in git migrates
// with no conversion step.
func ParseOverrideSet(s string) OverrideSet {
	set := NewOverrideSet()
	var section string
	for _, raw := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if key, _, value := splitEntry(raw); key != "" && section != "" {
			set.Set(section, key, value)
		}
	}
	return set
}
