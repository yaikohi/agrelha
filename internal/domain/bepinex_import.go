package domain

import (
	"fmt"
	"sort"
	"strings"
)

type ImportStatus string

const (
	ImportApply     ImportStatus = "apply"
	ImportSame      ImportStatus = "same"
	ImportUnmatched ImportStatus = "unmatched"
	ImportRejected  ImportStatus = "rejected"
)

type ImportEntry struct {
	Section string
	Name    string
	Value   string
	Current string
	Default string
	Status  ImportStatus
	Reason  string
}

func (e ImportEntry) Key() SettingKey { return SettingKey{Section: e.Section, Name: e.Name} }

type ImportPlan struct {
	PluginGUID    string
	PluginName    string
	PluginVersion string
	Entries       []ImportEntry
	Parsed        bool
}

func (p ImportPlan) Count(status ImportStatus) int {
	n := 0
	for _, e := range p.Entries {
		if e.Status == status {
			n++
		}
	}
	return n
}

func (p ImportPlan) Filter(status ImportStatus) []ImportEntry {
	var out []ImportEntry
	for _, e := range p.Entries {
		if e.Status == status {
			out = append(out, e)
		}
	}
	return out
}

func (p ImportPlan) Changes() bool {
	return p.Count(ImportApply) > 0 || p.Count(ImportUnmatched) > 0
}

func (p ImportPlan) Overrides() OverrideSet {
	set := NewOverrideSet()
	for _, e := range p.Entries {
		switch e.Status {
		case ImportApply, ImportUnmatched:
			set.Set(e.Section, e.Name, e.Value)
		}
	}
	return set
}

func PlanImport(generated ConfigFile, pasted string) ImportPlan {
	incoming := ParseConfigFile("", pasted)

	plan := ImportPlan{
		PluginGUID:    incoming.PluginGUID,
		PluginName:    incoming.PluginName,
		PluginVersion: incoming.PluginVersion,
		Parsed:        incoming.Count() > 0,
	}

	for _, in := range incoming.Settings() {
		entry := ImportEntry{Section: in.Section, Name: in.Name, Value: strings.TrimSpace(in.Value)}

		known, ok := generated.Lookup(in.Section, in.Name)
		if !ok {
			entry.Status = ImportUnmatched
			entry.Reason = "this mod does not declare it"
			plan.Entries = append(plan.Entries, entry)
			continue
		}

		entry.Current = strings.TrimSpace(known.Value)
		entry.Default = strings.TrimSpace(known.Default)

		baseline, hasBaseline := entry.Default, known.HasDefault
		if !hasBaseline {
			baseline = entry.Current
		}
		if entry.Value == baseline {
			entry.Status = ImportSame
			if hasBaseline {
				entry.Reason = "same as the mod's default"
			} else {
				entry.Reason = "same as the current value"
			}
			plan.Entries = append(plan.Entries, entry)
			continue
		}

		if err := known.Validate(entry.Value); err != nil {
			entry.Status = ImportRejected
			entry.Reason = err.Error()
			plan.Entries = append(plan.Entries, entry)
			continue
		}

		entry.Status = ImportApply
		plan.Entries = append(plan.Entries, entry)
	}

	sort.SliceStable(plan.Entries, func(i, j int) bool {
		if plan.Entries[i].Section != plan.Entries[j].Section {
			return plan.Entries[i].Section < plan.Entries[j].Section
		}
		return plan.Entries[i].Name < plan.Entries[j].Name
	})
	return plan
}

func MatchesConfig(generated ConfigFile, plan ImportPlan) error {
	if plan.PluginGUID == "" || generated.PluginGUID == "" {
		return nil
	}
	if !strings.EqualFold(plan.PluginGUID, generated.PluginGUID) {
		return fmt.Errorf("that file is for %s, not %s", plan.PluginGUID, generated.PluginGUID)
	}
	return nil
}
