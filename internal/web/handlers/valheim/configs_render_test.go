package valheim

import (
	"fmt"
	"strings"
	"testing"

	appbepinex "agrelha/internal/app/bepinex"
	"agrelha/internal/domain"
)

func viewOf(t *testing.T, body string, overrides ...domain.Override) appbepinex.FileView {
	t.Helper()
	set := domain.NewOverrideSet()
	for _, o := range overrides {
		set.Set(o.Section, o.Name, o.Value)
	}
	return appbepinex.FileView{
		Config:    domain.ParseConfigFile("test.cfg", body),
		Overrides: set,
		SHA256:    "abc123",
		Size:      int64(len(body)),
	}
}

const renderCfg = `## Settings file was created by plugin Mining v1.1.6
## Plugin GUID: org.bepinex.plugins.mining

[1 - General]

## If on, the configuration is locked.
# Setting type: Toggle
# Default value: On
# Acceptable values: Off, On
Lock Configuration = On

[2 - Mining]

## Mining yield factor at skill level 100.
# Setting type: Single
# Default value: 2
# Acceptable value range: From 1 to 5
Mining Yield Factor = 3
`

func TestToggleControlEmitsTheFilesOwnVocabulary(t *testing.T) {
	view := viewOf(t, renderCfg)
	s, ok := view.Config.Lookup("1 - General", "Lock Configuration")
	if !ok {
		t.Fatal("Lock Configuration not found")
	}
	got := renderControl(s, 0)
	if !strings.Contains(got, `type="checkbox"`) {
		t.Errorf("a toggle must be a checkbox, not a dropdown: %s", got)
	}
	if !strings.Contains(got, "'On'") || !strings.Contains(got, "'Off'") {
		t.Errorf("the checkbox must emit this file's On/Off words: %s", got)
	}
	if strings.Contains(got, "'true'") {
		t.Errorf("true/false would be silently ignored by this mod: %s", got)
	}
}

func TestNumberControlCarriesTheModsStatedRange(t *testing.T) {
	view := viewOf(t, renderCfg)
	s, _ := view.Config.Lookup("2 - Mining", "Mining Yield Factor")
	got := renderControl(s, 1)
	if !strings.Contains(got, `type="number"`) {
		t.Errorf("a Single with a range should be a number input: %s", got)
	}
	if !strings.Contains(got, `min="1"`) || !strings.Contains(got, `max="5"`) {
		t.Errorf("the mod's range must reach the input: %s", got)
	}
	if !strings.Contains(got, `step="any"`) {
		t.Errorf("a float setting must not be restricted to whole numbers: %s", got)
	}
}

func TestWholeNumberControlStepsByOne(t *testing.T) {
	s := domain.Setting{Type: "Int32", Value: "50"}
	if got := renderControl(s, 0); !strings.Contains(got, `step="1"`) {
		t.Errorf("an Int32 must step by 1: %s", got)
	}
}

func TestChoiceControlListsOnlyWhatTheModAccepts(t *testing.T) {
	s := domain.Setting{Type: "Mode", Acceptable: []string{"Alpha", "Beta"}, Value: "Beta"}
	got := renderControl(s, 0)
	if !strings.Contains(got, "<select") {
		t.Errorf("an enum should be a select: %s", got)
	}
	if !strings.Contains(got, `<option value="Beta" selected>`) {
		t.Errorf("the current value must be preselected: %s", got)
	}
}

func TestUndescribedSettingFallsBackToText(t *testing.T) {
	s := domain.Setting{Value: "0", Orphaned: true}
	if got := renderControl(s, 0); !strings.Contains(got, `type="text"`) {
		t.Errorf("a setting the mod never described must be free text: %s", got)
	}
}

func TestSettingRowShowsWhatChangedAndWhatIsManaged(t *testing.T) {
	view := viewOf(t, renderCfg, domain.Override{Section: "2 - Mining", Name: "Mining Yield Factor", Value: "3"})
	s, _ := view.Config.Lookup("2 - Mining", "Mining Yield Factor")
	got := renderSetting(2, "test.cfg", view, s, 1)

	if !strings.Contains(got, ">changed<") {
		t.Error("a value away from the mod's default must be marked")
	}
	if !strings.Contains(got, ">managed<") {
		t.Error("a value agrelha pins must be marked")
	}
	if !strings.Contains(got, "default: 2") {
		t.Error("the mod's default should be visible next to the control")
	}
	if !strings.Contains(got, "Reset to default") {
		t.Error("a setting with a known default should offer a reset")
	}
	if !strings.Contains(got, "Forget") {
		t.Error("a managed setting should offer to be forgotten")
	}

	if !strings.Contains(got, "keeps its current value") {
		t.Error("the forget confirmation must say the server keeps its value")
	}
}

func TestUnmanagedSettingOffersNoForget(t *testing.T) {
	view := viewOf(t, renderCfg)
	s, _ := view.Config.Lookup("2 - Mining", "Mining Yield Factor")
	if got := renderSetting(2, "test.cfg", view, s, 1); strings.Contains(got, "Forget") {
		t.Error("there is nothing to forget on a setting agrelha does not manage")
	}
}

func TestEmptyManagedListExplainsItselfRatherThanLookingBroken(t *testing.T) {
	view := viewOf(t, renderCfg)
	got := renderSettings(2, "test.cfg", view, nil, false, true)
	if !strings.Contains(got, "not managing any values") {
		t.Errorf("expected an explanation, got %s", got)
	}
	if got := renderSettings(2, "test.cfg", view, nil, false, false); !strings.Contains(got, "No settings match") {
		t.Errorf("a filtered empty result reads differently, got %s", got)
	}
}

func TestTruncationIsStatedNotHidden(t *testing.T) {
	view := viewOf(t, renderCfg)
	all := view.Config.Settings()
	got := renderSettings(2, "test.cfg", view, all, true, false)
	if !strings.Contains(got, "Narrow the search") {
		t.Errorf("a clipped list must say so, or it reads as 'no such setting': %s", got)
	}
}

func TestRawEditorHoldsTheOverrideSetNotTheGeneratedFile(t *testing.T) {
	big := strings.Builder{}
	big.WriteString("[Huge]\n\n")
	for i := 0; i < 5000; i++ {
		big.WriteString("## a description line that makes this file large\n")
		big.WriteString("# Setting type: Int32\n# Default value: 0\n")
		big.WriteString("Setting ")
		big.WriteString(strings.Repeat("x", 12))
		big.WriteString(" = 0\n")
	}
	body := big.String()
	if len(body) < 200_000 {
		t.Fatalf("test fixture should be large, got %d bytes", len(body))
	}

	view := viewOf(t, body, domain.Override{Section: "Huge", Name: "One", Value: "1"})
	got := renderRawEditor(2, "huge.cfg", view)

	if len(got) > 4096 {
		t.Errorf("the raw editor rendered %d bytes; it must contain the override set, not the %d-byte generated file", len(got), len(body))
	}
	if strings.Contains(got, "# Setting type:") {
		t.Error("the generated file's metadata must never reach the textarea")
	}
	if !strings.Contains(got, "One = 1") {
		t.Error("the override set should be what is editable")
	}
}

func TestPanelForAnUnparsableFileOffersTheRawEditor(t *testing.T) {
	view := viewOf(t, "this is not a config file\n")
	got := renderPanel(2, "weird.cfg", view)
	if !strings.Contains(got, "could not read this file's structure") {
		t.Errorf("an unparsable file must be explained, not silently empty: %s", got)
	}
	if !strings.Contains(got, "<textarea") {
		t.Error("expected the raw editor")
	}
}

func TestPanelDoesNotRenderEverySettingUpFront(t *testing.T) {
	var b strings.Builder
	b.WriteString("[S]\n\n")
	for i := 0; i < 800; i++ {
		b.WriteString("## description\n# Setting type: Int32\n# Default value: 0\n")
		b.WriteString("Setting ")
		b.WriteString(strings.Repeat("y", 10))
		b.WriteString(" = 0\n")
	}
	view := viewOf(t, b.String())
	got := renderPanel(3, "big.cfg", view)
	if strings.Count(got, `type="number"`) != 0 {
		t.Errorf("the panel rendered %d inputs before the operator asked for any", strings.Count(got, `type="number"`))
	}
	if !strings.Contains(got, "not managing any values") {
		t.Error("the panel should open on what agrelha manages")
	}
}

func TestNamesWithQuotesCannotBreakOutOfTheHandler(t *testing.T) {
	body := "[It's A Section]\n\n## d\n# Setting type: String\n# Default value: x\nIt's A Setting = y\n"
	view := viewOf(t, body, domain.Override{Section: "It's A Section", Name: "It's A Setting", Value: "y"})
	s, ok := view.Config.Lookup("It's A Section", "It's A Setting")
	if !ok {
		t.Fatal("setting not found")
	}
	got := renderSetting(1, "q.cfg", view, s, 0)

	if !strings.Contains(got, `It\&#39;s A Section`) {
		t.Errorf("apostrophe not escaped for the JS literal: %s", got)
	}
	if strings.Contains(got, `section: &#39;It&#39;s`) {
		t.Error("an unescaped apostrophe would terminate the JS string early")
	}
}

func TestHumanBytes(t *testing.T) {
	for _, tc := range []struct {
		in   int64
		want string
	}{{512, "512 B"}, {2048, "2 KB"}, {207370, "203 KB"}, {2 << 20, "2.0 MB"}} {
		if got := humanBytes(tc.in); got != tc.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLongDescriptionsAreTruncatedForTheRow(t *testing.T) {
	long := strings.Repeat("word ", 200)
	if got := truncate(long, 240); len(got) > 245 {
		t.Errorf("description not truncated: %d chars", len(got))
	}
	if got := truncate("short", 240); got != "short" {
		t.Errorf("a short description must pass through unchanged, got %q", got)
	}
	if strings.Contains(truncate("a\nb", 240), "\n") {
		t.Error("newlines must not survive into a single-line row")
	}
}

func TestControlsDoNotBindUntouchedSettingsIntoThePayload(t *testing.T) {
	for _, s := range []domain.Setting{
		{Type: "Int32", Value: "5"},
		{Type: "Single", Value: "1.5", Range: &domain.SettingRange{From: "1", To: "5"}},
		{Type: "String", Value: "text"},
		{Type: "Mode", Acceptable: []string{"A", "B"}, Value: "A"},
		{Type: "LogChannel", Acceptable: []string{"Info", "Warn"}, MultipleValues: true, Value: "Warn"},
		{Type: "Boolean", Value: "true"},
	} {
		got := renderControl(s, 7)
		if strings.Contains(got, "data-bind") {
			t.Errorf("%s uses data-bind, which submits it untouched: %s", s.Type, got)
		}
		if !strings.Contains(got, "$cfgDirty['7']") {
			t.Errorf("%s must write its own signal on change: %s", s.Type, got)
		}
	}
}

func TestEditedControlsStillCarryTheirCurrentValue(t *testing.T) {
	if got := renderControl(domain.Setting{Type: "Int32", Value: "42"}, 0); !strings.Contains(got, `value="42"`) {
		t.Errorf("number input lost its current value: %s", got)
	}
	if got := renderControl(domain.Setting{Type: "String", Value: "hello"}, 0); !strings.Contains(got, `value="hello"`) {
		t.Errorf("text input lost its current value: %s", got)
	}
	if got := renderControl(domain.Setting{Type: "M", Acceptable: []string{"A", "B"}, Value: "B"}, 0); !strings.Contains(got, `<option value="B" selected>`) {
		t.Errorf("select lost its current value: %s", got)
	}
}

func TestImportPreviewSeparatesWhatWillAndWillNotBeWritten(t *testing.T) {
	view := viewOf(t, renderCfg)
	plan := domain.PlanImport(view.Config, `[2 - Mining]
Mining Yield Factor = 4

[1 - General]
Lock Configuration = On

[Gone]
Old = 1
`)
	got := renderImportPreview(2, "test.cfg", view, plan)

	if !strings.Contains(got, "1</span> to import") {
		t.Errorf("the count of real changes must be stated: %s", got)
	}
	if !strings.Contains(got, "1 already default") {
		t.Error("values matching the default should be reported as skipped")
	}
	if !strings.Contains(got, "1 not declared by this mod") {
		t.Error("unmatched entries must be visible before importing")
	}
	if !strings.Contains(got, "3 → <span class=\"text-zinc-200\">4</span>") {
		t.Errorf("the preview must show current → new: %s", got)
	}
}

func TestImportPreviewOfNonsenseSaysSo(t *testing.T) {
	view := viewOf(t, renderCfg)
	plan := domain.PlanImport(view.Config, "just some text")
	got := renderImportPreview(2, "test.cfg", view, plan)
	if !strings.Contains(got, "does not look like a BepInEx config") && !strings.Contains(got, "looks like a BepInEx config") {
		t.Errorf("expected an explanation, got %s", got)
	}
}

func TestImportPreviewSaysWhenThereIsNothingToDo(t *testing.T) {
	pristine := strings.Replace(renderCfg, "Mining Yield Factor = 3", "Mining Yield Factor = 2", 1)
	view := viewOf(t, pristine)
	plan := domain.PlanImport(view.Config, pristine)
	got := renderImportPreview(2, "test.cfg", view, plan)
	if !strings.Contains(got, "Nothing to import") {
		t.Errorf("importing a mod's own untouched config changes nothing and must say so: %s", got)
	}
}

func TestImportPreviewCapsTheRowsItRenders(t *testing.T) {
	var gen strings.Builder
	gen.WriteString("[S]\n\n")
	var paste strings.Builder
	paste.WriteString("[S]\n")
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&gen, "## d\n# Setting type: Int32\n# Default value: 0\nKey%03d = 0\n", i)
		fmt.Fprintf(&paste, "Key%03d = 1\n", i)
	}
	view := viewOf(t, gen.String())
	plan := domain.PlanImport(view.Config, paste.String())
	if plan.Count(domain.ImportApply) != 300 {
		t.Fatalf("fixture wrong: %d", plan.Count(domain.ImportApply))
	}
	got := renderImportPreview(2, "test.cfg", view, plan)
	if strings.Count(got, "Key") > 60 {
		t.Errorf("the preview rendered too many rows: %d", strings.Count(got, "Key"))
	}
	if !strings.Contains(got, "and 260 more") {
		t.Errorf("the elision must be stated: %s", got)
	}
}

func TestImportPanelOffersBothPasteAndFile(t *testing.T) {
	got := renderImportPanel(3)
	if !strings.Contains(got, `type="file"`) {
		t.Error("expected a file picker")
	}
	if !strings.Contains(got, "<textarea") {
		t.Error("expected a paste box")
	}
	if !strings.Contains(got, "configs/import/preview") || !strings.Contains(got, "configs/import/apply") {
		t.Error("expected both the check and the import action")
	}
	if !strings.Contains(got, "only the values that differ") {
		t.Error("the panel must say what it will actually store")
	}
}
