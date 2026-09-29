package domain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The golden files are real configs pulled off instance-02, not invented ones.
// A parser for this format is only worth anything if it survives what the mods
// actually write.
func realConfigs(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join("testdata", "bepinex")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	out := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".cfg") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		out[e.Name()] = string(b)
	}
	if len(out) == 0 {
		t.Fatal("no golden configs found")
	}
	return out
}

func TestParseRendersBackByteForByte(t *testing.T) {
	for name, body := range realConfigs(t) {
		t.Run(name, func(t *testing.T) {
			got := ParseConfigFile(name, body).Render()
			if got != body {
				t.Errorf("round trip changed the file\n--- got ---\n%q\n--- want ---\n%q", got, body)
			}
		})
	}
}

func TestParseReadsTheMetadataMoosDoWrite(t *testing.T) {
	cfgs := realConfigs(t)
	f := ParseConfigFile("mining", cfgs["org.bepinex.plugins.mining.cfg"])

	if f.PluginName != "Mining" || f.PluginVersion != "1.1.6" {
		t.Errorf("plugin header = %q %q, want Mining 1.1.6", f.PluginName, f.PluginVersion)
	}
	if f.PluginGUID != "org.bepinex.plugins.mining" {
		t.Errorf("plugin guid = %q", f.PluginGUID)
	}
	if !f.Parsable() {
		t.Fatalf("a real mod config must parse; warnings: %+v", f.Warnings)
	}

	yield, ok := f.Lookup("2 - Mining", "Mining Yield Factor")
	if !ok {
		t.Fatalf("Mining Yield Factor not found; sections %v", f.SectionNames())
	}
	if yield.Type != "Single" {
		t.Errorf("type = %q, want Single", yield.Type)
	}
	if !yield.HasDefault || yield.Default != "2" {
		t.Errorf("default = %q (has=%v), want 2", yield.Default, yield.HasDefault)
	}
	if yield.Range == nil || yield.Range.From != "1" || yield.Range.To != "5" {
		t.Errorf("range = %+v, want 1..5", yield.Range)
	}
	if yield.Description == "" {
		t.Error("the mod's own description is the help text; it must survive parsing")
	}

	lock, ok := f.Lookup("1 - General", "Lock Configuration")
	if !ok {
		t.Fatal("Lock Configuration not found")
	}
	if got := lock.Acceptable; len(got) != 2 || got[0] != "Off" || got[1] != "On" {
		t.Errorf("acceptable = %v, want [Off On]", got)
	}
}

// r2modman never reads "# Default value:" at all, so it cannot say what an
// operator changed. That line is the whole basis of the "differs from default"
// marker, so assert it against a real file.
func TestDiffersFromDefaultUsesTheModsOwnDefault(t *testing.T) {
	cfgs := realConfigs(t)

	// Every mod config was captured at its defaults, because the sync bug meant
	// nothing committed to git ever reached the disk. So a pristine file must
	// report nothing changed...
	for name, body := range cfgs {
		if name == "BepInEx.cfg" {
			continue
		}
		f := ParseConfigFile(name, body)
		if n := f.ChangedCount(); n != 0 {
			t.Errorf("%s: untouched mod config reports %d changed settings", name, n)
		}
	}

	// ...while BepInEx's own config is genuinely tuned by the lloesche image
	// (console Enabled, PreventClose, ForceBepInExTTYDriver, WriteUnityLog are
	// all flipped from false). Real drift on a real file, detected from nothing
	// but the "# Default value:" lines.
	if n := ParseConfigFile("BepInEx.cfg", cfgs["BepInEx.cfg"]).ChangedCount(); n != 4 {
		t.Errorf("BepInEx.cfg changed settings = %d, want the 4 the image sets", n)
	}

	// ...and the same file with one value moved must report exactly that one.
	body := strings.Replace(
		cfgs["org.bepinex.plugins.mining.cfg"],
		"Mining Yield Factor = 2",
		"Mining Yield Factor = 3",
		1)
	f := ParseConfigFile("mining", body)

	yield, ok := f.Lookup("2 - Mining", "Mining Yield Factor")
	if !ok {
		t.Fatal("Mining Yield Factor not found")
	}
	if !yield.DiffersFromDefault() {
		t.Errorf("value %q against default %q should read as changed", yield.Value, yield.Default)
	}
	damage, ok := f.Lookup("2 - Mining", "Mining Damage Factor")
	if !ok {
		t.Fatal("Mining Damage Factor not found")
	}
	if damage.DiffersFromDefault() {
		t.Error("an untouched sibling setting must not report as changed")
	}
	if n := f.ChangedCount(); n != 1 {
		t.Errorf("ChangedCount = %d, want exactly 1", n)
	}
}

func TestSettingWithNoDefaultLineClaimsNothing(t *testing.T) {
	f := ParseConfigFile("x", "[S]\n\nKey = 5\n")
	s, ok := f.Lookup("S", "Key")
	if !ok {
		t.Fatal("Key not found")
	}
	if s.HasDefault {
		t.Error("no default line means no default")
	}
	if s.DiffersFromDefault() {
		t.Error("a setting with no known default must not claim to differ from it")
	}
	if !s.Orphaned {
		t.Error("an entry with no description block is what BepInEx writes for an orphan")
	}
}

func TestControlKindForTheTypesModsActuallyUse(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    Setting
		want ControlKind
	}{
		{"boolean", Setting{Type: "Boolean"}, ControlToggle},
		{"serversync toggle", Setting{Type: "Toggle", Acceptable: []string{"Off", "On"}}, ControlToggle},
		{"enum", Setting{Type: "Mode", Acceptable: []string{"Alpha", "Beta", "Gamma"}}, ControlChoice},
		{"flags enum", Setting{Type: "LogLevel", Acceptable: []string{"Info", "Debug"}, MultipleValues: true}, ControlMulti},
		{"int", Setting{Type: "Int32"}, ControlNumber},
		{"float", Setting{Type: "Single"}, ControlNumber},
		{"shortcut", Setting{Type: "KeyboardShortcut"}, ControlShortcut},
		{"string", Setting{Type: "String"}, ControlText},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.Control(); got != tc.want {
				t.Errorf("Control() = %q, want %q", got, tc.want)
			}
		})
	}
}

// A Toggle writes On/Off and a Boolean writes true/false. Writing the wrong
// pair produces a value the mod quietly ignores, which looks like the setting
// having no effect.
func TestToggleUsesTheFilesOwnVocabulary(t *testing.T) {
	serverSync := Setting{Type: "Toggle", Acceptable: []string{"Off", "On"}, Value: "On"}
	if serverSync.TrueValue() != "On" || serverSync.FalseValue() != "Off" {
		t.Errorf("ServerSync toggle should speak On/Off, got %q/%q", serverSync.TrueValue(), serverSync.FalseValue())
	}
	if !serverSync.IsOn() {
		t.Error("On should read as enabled")
	}
	plain := Setting{Type: "Boolean", Value: "false"}
	if plain.TrueValue() != "true" || plain.FalseValue() != "false" {
		t.Errorf("Boolean should speak true/false, got %q/%q", plain.TrueValue(), plain.FalseValue())
	}
	if plain.IsOn() {
		t.Error("false should read as disabled")
	}
}

func TestValidateRejectsWhatTheModSaidItWontTake(t *testing.T) {
	ranged := Setting{Type: "Single", Range: &SettingRange{From: "1", To: "5"}}
	if err := ranged.Validate("3"); err != nil {
		t.Errorf("3 is inside 1..5: %v", err)
	}
	if err := ranged.Validate("9"); err == nil {
		t.Error("9 is outside 1..5 and must be refused")
	}
	if err := ranged.Validate("banana"); err == nil {
		t.Error("a non-number must be refused for a numeric setting")
	}

	enum := Setting{Type: "Toggle", Acceptable: []string{"Off", "On"}}
	if err := enum.Validate("On"); err != nil {
		t.Errorf("On is acceptable: %v", err)
	}
	if err := enum.Validate("Maybe"); err == nil {
		t.Error("a value outside the acceptable list must be refused")
	}

	whole := Setting{Type: "Int32"}
	if err := whole.Validate("1.5"); err == nil {
		t.Error("Int32 must refuse a fractional value")
	}

	free := Setting{Type: "String"}
	if err := free.Validate("anything at all"); err != nil {
		t.Errorf("a String setting takes any text: %v", err)
	}
}

func TestSearchCapsResultsAndSaysSo(t *testing.T) {
	f := ParseConfigFile("x", "[S]\n\nAlpha = 1\nAlphabet = 2\nBeta = 3\n")
	hits, truncated := f.Search(SearchQuery{Text: "alpha"})
	if len(hits) != 2 || truncated {
		t.Errorf("want 2 untruncated hits, got %d truncated=%v", len(hits), truncated)
	}
	hits, truncated = f.Search(SearchQuery{Text: "alpha", Limit: 1})
	if len(hits) != 1 || !truncated {
		t.Errorf("want 1 hit and a truncation flag, got %d truncated=%v", len(hits), truncated)
	}
	if _, truncated := f.Search(SearchQuery{Limit: 3}); truncated {
		t.Error("a limit exactly equal to the result count is not truncation")
	}
}

func TestSearchOnlyChangedNeedsAKnownDefault(t *testing.T) {
	cfgs := realConfigs(t)
	f := ParseConfigFile("mining", cfgs["org.bepinex.plugins.mining.cfg"])
	hits, _ := f.Search(SearchQuery{OnlyChanged: true})
	if len(hits) != f.ChangedCount() {
		t.Errorf("OnlyChanged returned %d, ChangedCount says %d", len(hits), f.ChangedCount())
	}
	for _, s := range hits {
		if !s.DiffersFromDefault() {
			t.Errorf("%s does not differ from its default but was returned", s.Key())
		}
	}
}

func TestUnparsableFileRefusesTheTypedEditor(t *testing.T) {
	f := ParseConfigFile("weird", "this is not a config\nneither is this\n")
	if f.Parsable() {
		t.Error("a file we did not understand must not get a typed form")
	}
	if len(f.Warnings) == 0 {
		t.Error("expected warnings naming the lines we could not read")
	}
	if f.Render() != "this is not a config\nneither is this\n" {
		t.Error("an unparsable file must still round-trip exactly")
	}
}

func TestEntryBeforeAnySectionIsRefusedNotGuessed(t *testing.T) {
	// BepInEx always writes a [Section] first. A bare entry means this is not
	// the format we think it is, and inventing a section would write the
	// Override somewhere the mod never reads.
	f := ParseConfigFile("x", "Key = 1\n\n[S]\n\nOther = 2\n")
	if f.Parsable() {
		t.Error("expected the file to be treated as unparsable")
	}
	if _, ok := f.Lookup("", "Key"); ok {
		t.Error("the section-less entry must not be attached to a made-up section")
	}
}

func TestValueMayContainEqualsSigns(t *testing.T) {
	f := ParseConfigFile("x", "[S]\n\nKey = a=b=c\n")
	s, ok := f.Lookup("S", "Key")
	if !ok {
		t.Fatal("Key not found")
	}
	if s.Value != "a=b=c" {
		t.Errorf("value = %q, want a=b=c", s.Value)
	}
}

func TestEmptyFileParsesAndRendersEmpty(t *testing.T) {
	// MaddCatter.WayfarerRecall.state.cfg on the live server is 0 bytes.
	f := ParseConfigFile("empty", "")
	if f.Parsable() {
		t.Error("an empty file has nothing to edit as a form")
	}
	if f.Render() != "" {
		t.Errorf("empty must render empty, got %q", f.Render())
	}
}

func TestCRLFIsNormalisedNotDuplicated(t *testing.T) {
	f := ParseConfigFile("x", "[S]\r\n\r\nKey = 1\r\n")
	s, ok := f.Lookup("S", "Key")
	if !ok {
		t.Fatal("Key not found")
	}
	if s.Value != "1" {
		t.Errorf("value = %q, want 1 with no stray carriage return", s.Value)
	}
}

func TestOverrideSetEncodesCanonically(t *testing.T) {
	a := NewOverrideSet()
	a.Set("Zulu", "b", "2")
	a.Set("Alpha", "z", "1")
	a.Set("Alpha", "a", "0")

	b := NewOverrideSet()
	b.Set("Alpha", "a", "0")
	b.Set("Zulu", "b", "2")
	b.Set("Alpha", "z", "1")

	if a.Encode() != b.Encode() {
		t.Errorf("insertion order must not change the bytes:\n%q\n%q", a.Encode(), b.Encode())
	}
	want := "[Alpha]\na = 0\nz = 1\n\n[Zulu]\nb = 2\n"
	if a.Encode() != want {
		t.Errorf("encode = %q, want %q", a.Encode(), want)
	}
}

func TestEmptyOverrideSetEncodesToNothing(t *testing.T) {
	// The caller deletes the ConfigMap key on empty. An empty value would make
	// the merge tool write an empty file over a good one.
	o := NewOverrideSet()
	if o.Encode() != "" {
		t.Errorf("want empty, got %q", o.Encode())
	}
}

func TestOverrideSetRoundTrips(t *testing.T) {
	o := NewOverrideSet()
	o.Set("2 - Mining", "Mining Yield Factor", "3")
	o.Set("1 - General", "Lock Configuration", "On")

	back := ParseOverrideSet(o.Encode())
	if back.Len() != 2 {
		t.Fatalf("want 2 overrides, got %d", back.Len())
	}
	if v, ok := back.Get("2 - Mining", "Mining Yield Factor"); !ok || v != "3" {
		t.Errorf("lost the mining override: %q %v", v, ok)
	}
}

// The one config already in git is a whole file, not a fragment. It has to keep
// working without a migration step.
func TestAWholeConfigFileIsAValidOverrideSet(t *testing.T) {
	cfgs := realConfigs(t)
	set := ParseOverrideSet(cfgs["org.bepinex.plugins.mining.cfg"])
	if set.Len() == 0 {
		t.Fatal("a full config must parse as an override set")
	}
	if v, ok := set.Get("2 - Mining", "Mining Yield Factor"); !ok || v != "2" {
		t.Errorf("want the file's own value, got %q %v", v, ok)
	}
	if _, ok := set.Get("1 - General", "Lock Configuration"); !ok {
		t.Error("every key in the file becomes an override, across all sections")
	}
}

// Forgetting an Override stops agrelha managing the Setting. It does NOT put
// the default back - the Generated config keeps whatever value was last written
// to it. Getting this wrong produces a button that visibly does nothing.
func TestUnsetStopsManagingButRestoresNothing(t *testing.T) {
	o := NewOverrideSet()
	o.Set("2 - Mining", "Mining Yield Factor", "4")

	if !o.Unset("2 - Mining", "Mining Yield Factor") {
		t.Error("Unset should report that it removed something")
	}
	if o.Unset("2 - Mining", "Mining Yield Factor") {
		t.Error("Unset on an absent key must report false")
	}
	if o.Has("2 - Mining", "Mining Yield Factor") {
		t.Error("the override should be gone")
	}

	// The file still reads 4 afterwards: nothing rewrote it.
	generated := ParseConfigFile("mining", "[2 - Mining]\n\n## d\n# Setting type: Single\n# Default value: 2\nMining Yield Factor = 4\n")
	merged, report := Merge(generated, o)
	if report.Changed() {
		t.Errorf("an emptied override set must not rewrite the file: %+v", report)
	}
	if !strings.Contains(merged, "Mining Yield Factor = 4") {
		t.Error("forgetting an override must leave the last written value in place, not restore the default")
	}
}

func TestValidateAcceptsSeveralFlagValuesAtOnce(t *testing.T) {
	flags := Setting{Type: "LogChannel", MultipleValues: true,
		Acceptable: []string{"None", "Info", "IL", "Warn", "Error", "Debug", "All"}}

	if err := flags.Validate("Warn, Error"); err != nil {
		t.Errorf("a flags enum takes a comma-separated list: %v", err)
	}
	if err := flags.Validate(""); err != nil {
		t.Errorf("an empty flags value means none set: %v", err)
	}
	if err := flags.Validate("Warn, Nonsense"); err == nil {
		t.Error("one bad member must fail the whole value")
	}
}

func TestMalformedMetadataIsIgnoredNotGuessed(t *testing.T) {
	f := ParseConfigFile("x", "[S]\n\n# Setting type: Single\n# Acceptable value range: nonsense\nKey = 1\n")
	s, ok := f.Lookup("S", "Key")
	if !ok {
		t.Fatal("Key not found")
	}
	if s.Range != nil {
		t.Errorf("an unreadable range must be dropped, not invented: %+v", s.Range)
	}
	if err := s.Validate("999"); err != nil {
		t.Errorf("with no usable range, any number is acceptable: %v", err)
	}
}

func TestPluginHeaderWithoutAVersion(t *testing.T) {
	f := ParseConfigFile("x", "## Settings file was created by plugin Nameless\n\n[S]\n\nKey = 1\n")
	if f.PluginName != "Nameless" || f.PluginVersion != "" {
		t.Errorf("name/version = %q/%q, want Nameless and no version", f.PluginName, f.PluginVersion)
	}
}

func TestPluginNamesMayContainSpaces(t *testing.T) {
	f := ParseConfigFile("x", "## Settings file was created by plugin Some Long Name v2.0.1\n\n[S]\n\nKey = 1\n")
	if f.PluginName != "Some Long Name" || f.PluginVersion != "2.0.1" {
		t.Errorf("name/version = %q/%q", f.PluginName, f.PluginVersion)
	}
}

func TestFileLevelAccessors(t *testing.T) {
	f := ParseConfigFile("x", "[Alpha]\n\nOne = 1\n\n[Beta]\n\nTwo = 2\nThree = 3\n")
	if got := f.Count(); got != 3 {
		t.Errorf("Count = %d, want 3", got)
	}
	if got := len(f.Settings()); got != 3 {
		t.Errorf("Settings() = %d, want 3", got)
	}
	if got := f.SectionNames(); len(got) != 2 || got[0] != "Alpha" || got[1] != "Beta" {
		t.Errorf("SectionNames = %v, want [Alpha Beta] in file order", got)
	}
	if got := (SettingKey{Section: "Alpha", Name: "One"}).String(); got != "[Alpha] One" {
		t.Errorf("SettingKey.String = %q", got)
	}
}

func TestOverrideSetSetOnAZeroValueIsSafe(t *testing.T) {
	// The zero OverrideSet has a nil map; writing to it must not panic.
	var o OverrideSet
	o.Set("S", "K", "1")
	if v, ok := o.Get("S", "K"); !ok || v != "1" {
		t.Errorf("got %q %v", v, ok)
	}
}

func TestMergeKeepsEverythingItDidNotChange(t *testing.T) {
	cfgs := realConfigs(t)
	body := cfgs["org.bepinex.plugins.mining.cfg"]
	f := ParseConfigFile("mining", body)

	o := NewOverrideSet()
	o.Set("2 - Mining", "Mining Yield Factor", "4")

	merged, report := Merge(f, o)

	if len(report.Applied) != 1 || len(report.Appended) != 0 || len(report.Stale) != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}

	// This is the r2modman bug, asserted absent: it filters out everything
	// before the first [Section] and so drops this header on every save.
	if !strings.Contains(merged, "## Settings file was created by plugin Mining v1.1.6") {
		t.Error("the file header must survive a merge")
	}
	if !strings.Contains(merged, "## Plugin GUID: org.bepinex.plugins.mining") {
		t.Error("the plugin GUID line must survive a merge")
	}
	if !strings.Contains(merged, "# Default value: 2") {
		t.Error("the metadata comments must survive a merge")
	}
	if !strings.Contains(merged, "Mining Yield Factor = 4") {
		t.Error("the override was not applied")
	}

	before := strings.Split(body, "\n")
	after := strings.Split(merged, "\n")
	if len(before) != len(after) {
		t.Fatalf("merge changed the line count %d -> %d", len(before), len(after))
	}
	changed := 0
	for i := range before {
		if before[i] != after[i] {
			changed++
		}
	}
	if changed != 1 {
		t.Errorf("merge touched %d lines, want exactly 1", changed)
	}
}

func TestMergeIsIdempotent(t *testing.T) {
	cfgs := realConfigs(t)
	f := ParseConfigFile("mining", cfgs["org.bepinex.plugins.mining.cfg"])
	o := NewOverrideSet()
	o.Set("2 - Mining", "Mining Yield Factor", "4")

	once, _ := Merge(f, o)
	twice, report := Merge(ParseConfigFile("mining", once), o)
	if once != twice {
		t.Error("merging an already-merged file must change nothing")
	}
	if report.Changed() {
		t.Errorf("second merge should be a no-op, got %+v", report)
	}
	if len(report.NoOp) != 1 {
		t.Errorf("want the override reported as already applied, got %+v", report)
	}
}

func TestMergeAppendsAnUnknownKeyUnderItsOwnSection(t *testing.T) {
	f := ParseConfigFile("x", "[Alpha]\n\nOne = 1\n\n[Beta]\n\nTwo = 2\n")
	o := NewOverrideSet()
	o.Set("Alpha", "Three", "3")

	merged, report := Merge(f, o)
	if len(report.Appended) != 1 || len(report.Stale) != 0 {
		t.Fatalf("want one appended override, got %+v", report)
	}

	// Bound to [Alpha], not dumped at the end of the file where BepInEx would
	// read it as part of [Beta].
	alpha := strings.Index(merged, "[Alpha]")
	beta := strings.Index(merged, "[Beta]")
	three := strings.Index(merged, "Three = 3")
	if three < alpha || three > beta {
		t.Errorf("appended entry landed outside its section:\n%s", merged)
	}
}

func TestMergeKeepsAnOverrideForASettingTheModDropped(t *testing.T) {
	// A mod that removes a Setting, or is uninstalled for an afternoon, must not
	// silently destroy a recorded decision.
	f := ParseConfigFile("x", "[Alpha]\n\nOne = 1\n")
	o := NewOverrideSet()
	o.Set("Gone", "Old", "7")

	merged, report := Merge(f, o)
	if len(report.Stale) != 1 {
		t.Fatalf("want the override reported stale, got %+v", report)
	}
	if !strings.Contains(merged, "Old = 7") {
		t.Error("a stale override must still be written, not discarded")
	}
}

func TestMergeOnAFreshPVCWritesABareConfig(t *testing.T) {
	// First boot: no mod has run, so there is no generated file to merge into.
	// BepInEx reads a bare file fine and rewrites it with full metadata on its
	// first Save(), keeping these values.
	o := NewOverrideSet()
	o.Set("2 - Mining", "Mining Yield Factor", "3")

	merged, report := Merge(ParseConfigFile("mining", ""), o)
	if merged != "[2 - Mining]\nMining Yield Factor = 3\n" {
		t.Errorf("unexpected bare config: %q", merged)
	}
	if len(report.Stale) != 1 {
		t.Errorf("with nothing to merge into, every override is unmatched: %+v", report)
	}
}

func TestMergeWithNoOverridesIsTheIdentity(t *testing.T) {
	for name, body := range realConfigs(t) {
		t.Run(name, func(t *testing.T) {
			merged, report := Merge(ParseConfigFile(name, body), NewOverrideSet())
			if merged != body {
				t.Error("an empty override set must leave the file byte-identical")
			}
			if report.Changed() {
				t.Errorf("nothing to do, but report says %+v", report)
			}
		})
	}
}

func TestMergePreservesTheSeparatorAsWritten(t *testing.T) {
	f := ParseConfigFile("x", "[S]\n\nKey=1\n")
	o := NewOverrideSet()
	o.Set("S", "Key", "2")
	merged, _ := Merge(f, o)
	if !strings.Contains(merged, "Key=2") {
		t.Errorf("a file written without spaces must stay that way: %q", merged)
	}
}
