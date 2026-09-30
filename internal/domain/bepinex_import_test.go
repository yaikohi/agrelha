package domain

import (
	"strings"
	"testing"
)

const importGenerated = `## Settings file was created by plugin Mining v1.1.6
## Plugin GUID: org.bepinex.plugins.mining

[1 - General]

## Lock it.
# Setting type: Toggle
# Default value: On
# Acceptable values: Off, On
Lock Configuration = On

[2 - Mining]

## Yield.
# Setting type: Single
# Default value: 2
# Acceptable value range: From 1 to 5
Mining Yield Factor = 2

## Damage.
# Setting type: Single
# Default value: 3
# Acceptable value range: From 1 to 10
Mining Damage Factor = 3

Undescribed Thing = 7
`

func TestImportStoresOnlyWhatDiffersFromTheDefault(t *testing.T) {
	gen := ParseConfigFile("mining.cfg", importGenerated)
	pasted := `## Plugin GUID: org.bepinex.plugins.mining

[1 - General]
Lock Configuration = On

[2 - Mining]
Mining Yield Factor = 4
Mining Damage Factor = 3
`
	plan := PlanImport(gen, pasted)

	if got := plan.Count(ImportApply); got != 1 {
		t.Fatalf("want 1 setting applied, got %d: %+v", got, plan.Entries)
	}
	if got := plan.Count(ImportSame); got != 2 {
		t.Errorf("want 2 settings skipped as default, got %d", got)
	}

	set := plan.Overrides()
	if set.Len() != 1 {
		t.Fatalf("only the deliberate change becomes an override, got %d", set.Len())
	}
	if v, ok := set.Get("2 - Mining", "Mining Yield Factor"); !ok || v != "4" {
		t.Errorf("wrong override: %q %v", v, ok)
	}
}

func TestImportOfAWholeUntouchedConfigStoresNothing(t *testing.T) {
	gen := ParseConfigFile("mining.cfg", importGenerated)
	plan := PlanImport(gen, importGenerated)

	if plan.Changes() {
		t.Errorf("a config identical to the mod's defaults records no decisions: %+v", plan.Entries)
	}
	if plan.Overrides().Len() != 0 {
		t.Error("nothing should be pinned")
	}
}

func TestImportRejectsValuesTheModWontTakeButKeepsTheRest(t *testing.T) {
	gen := ParseConfigFile("mining.cfg", importGenerated)
	pasted := `[2 - Mining]
Mining Yield Factor = 99
Mining Damage Factor = 8
`
	plan := PlanImport(gen, pasted)

	if got := plan.Count(ImportRejected); got != 1 {
		t.Fatalf("want 1 rejected, got %d: %+v", got, plan.Entries)
	}
	if got := plan.Count(ImportApply); got != 1 {
		t.Fatalf("the valid entry must still import, got %d", got)
	}
	rejected := plan.Filter(ImportRejected)[0]
	if rejected.Name != "Mining Yield Factor" || !strings.Contains(rejected.Reason, "range") {
		t.Errorf("the reason should name the constraint: %+v", rejected)
	}
	if _, ok := plan.Overrides().Get("2 - Mining", "Mining Yield Factor"); ok {
		t.Error("a rejected value must not be written")
	}
}

func TestImportKeepsSettingsTheModDoesNotDeclare(t *testing.T) {
	gen := ParseConfigFile("mining.cfg", importGenerated)
	pasted := "[Gone]\nOld Setting = 5\n"
	plan := PlanImport(gen, pasted)

	if got := plan.Count(ImportUnmatched); got != 1 {
		t.Fatalf("want 1 unmatched, got %d", got)
	}
	if _, ok := plan.Overrides().Get("Gone", "Old Setting"); !ok {
		t.Error("an unmatched entry is still a recorded decision and must be importable")
	}
}

func TestImportComparesAgainstCurrentWhenThereIsNoDefault(t *testing.T) {
	gen := ParseConfigFile("mining.cfg", importGenerated)

	same := PlanImport(gen, "[2 - Mining]\nUndescribed Thing = 7\n")
	if same.Count(ImportSame) != 1 {
		t.Errorf("an undescribed setting matching the current value is not a change: %+v", same.Entries)
	}

	diff := PlanImport(gen, "[2 - Mining]\nUndescribed Thing = 9\n")
	if diff.Count(ImportApply) != 1 {
		t.Errorf("an undescribed setting with a new value is a change: %+v", diff.Entries)
	}
}

func TestImportReportsWhatTheFileSaysItIs(t *testing.T) {
	gen := ParseConfigFile("mining.cfg", importGenerated)
	plan := PlanImport(gen, importGenerated)

	if plan.PluginGUID != "org.bepinex.plugins.mining" {
		t.Errorf("guid = %q", plan.PluginGUID)
	}
	if plan.PluginName != "Mining" || plan.PluginVersion != "1.1.6" {
		t.Errorf("plugin = %q %q", plan.PluginName, plan.PluginVersion)
	}
	if !plan.Parsed {
		t.Error("a real config should parse")
	}
}

func TestImportOfNonsenseParsesNothing(t *testing.T) {
	gen := ParseConfigFile("mining.cfg", importGenerated)
	plan := PlanImport(gen, "this is not a config at all\n")
	if plan.Parsed {
		t.Error("nothing was parsed, and the UI must be able to say so")
	}
	if len(plan.Entries) != 0 {
		t.Errorf("no entries expected, got %+v", plan.Entries)
	}
}

func TestImportRefusesAFileForADifferentMod(t *testing.T) {
	gen := ParseConfigFile("mining.cfg", importGenerated)
	other := PlanImport(gen, "## Plugin GUID: com.someone.else\n\n[S]\nK = 1\n")
	if err := MatchesConfig(gen, other); err == nil {
		t.Error("a config from another mod must be refused by GUID")
	}

	right := PlanImport(gen, importGenerated)
	if err := MatchesConfig(gen, right); err != nil {
		t.Errorf("the mod's own config must be accepted: %v", err)
	}

	headerless := PlanImport(gen, "[2 - Mining]\nMining Yield Factor = 4\n")
	if err := MatchesConfig(gen, headerless); err != nil {
		t.Errorf("a fragment with no header cannot be checked and must not be refused: %v", err)
	}
}

func TestImportPlanIsOrdered(t *testing.T) {
	gen := ParseConfigFile("mining.cfg", importGenerated)
	plan := PlanImport(gen, `[2 - Mining]
Mining Yield Factor = 4

[1 - General]
Lock Configuration = Off
`)
	if len(plan.Entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(plan.Entries))
	}
	if plan.Entries[0].Section != "1 - General" {
		t.Errorf("entries should be ordered by section: %+v", plan.Entries)
	}
}

func TestImportCarriesCurrentAndDefaultForThePreview(t *testing.T) {
	gen := ParseConfigFile("mining.cfg", importGenerated)
	plan := PlanImport(gen, "[2 - Mining]\nMining Yield Factor = 4\n")
	e := plan.Filter(ImportApply)[0]
	if e.Current != "2" || e.Default != "2" || e.Value != "4" {
		t.Errorf("the preview needs all three: %+v", e)
	}
}

func TestAValueTheModShipsAsItsDefaultIsAlwaysAcceptable(t *testing.T) {
	s := Setting{
		Type:       "BuildPieceCategory",
		Default:    "0",
		HasDefault: true,
		Acceptable: []string{"Misc", "Crafting", "Building"},
		Value:      "0",
	}
	if err := s.Validate("0"); err != nil {
		t.Errorf("the mod ships 0 as its own default, so it cannot be invalid: %v", err)
	}
	if err := s.Validate("Nonsense"); err == nil {
		t.Error("a value that is neither the default nor acceptable must still be refused")
	}
	if err := s.Validate("Crafting"); err != nil {
		t.Errorf("a listed value must still pass: %v", err)
	}
}

func TestImportDoesNotValidateAValueItIsNotGoingToWrite(t *testing.T) {
	gen := ParseConfigFile("x.cfg", `[Bow rack]

## Category.
# Setting type: BuildPieceCategory
# Default value: 0
# Acceptable values: Misc, Crafting, Building
Build Menu Tags = 0
`)
	plan := PlanImport(gen, "[Bow rack]\nBuild Menu Tags = 0\n")
	if plan.Count(ImportRejected) != 0 {
		t.Errorf("an entry equal to the current value is a no-op and must not be validated: %+v", plan.Entries)
	}
	if plan.Count(ImportSame) != 1 {
		t.Errorf("want it skipped as unchanged, got %+v", plan.Entries)
	}
}
