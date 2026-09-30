package domain

import "strings"

type MergeReport struct {
	Applied []Override

	NoOp []Override

	Appended []Override

	Stale []Override
}

func (r MergeReport) Changed() bool {
	return len(r.Applied) > 0 || len(r.Appended) > 0 || len(r.Stale) > 0
}

func Merge(generated ConfigFile, o OverrideSet) (string, MergeReport) {
	var report MergeReport

	if len(generated.lines) == 0 && len(generated.Sections) == 0 {
		for _, ov := range o.All() {
			report.Stale = append(report.Stale, ov)
		}
		return o.Encode(), report
	}

	lines := make([]string, len(generated.lines))
	copy(lines, generated.lines)

	index := map[SettingKey]Setting{}
	for _, sec := range generated.Sections {
		for _, s := range sec.Settings {
			index[s.Key()] = s
		}
	}
	knownSections := map[string]bool{}
	for _, sec := range generated.Sections {
		knownSections[sec.Name] = true
	}

	var pending []Override
	for _, ov := range o.All() {
		s, ok := index[ov.Key()]
		if !ok {
			if knownSections[ov.Section] {
				report.Appended = append(report.Appended, ov)
			} else {
				report.Stale = append(report.Stale, ov)
			}
			pending = append(pending, ov)
			continue
		}
		if strings.TrimSpace(s.Value) == strings.TrimSpace(ov.Value) {
			report.NoOp = append(report.NoOp, ov)
			continue
		}
		lines[s.lineIndex] = s.Name + s.separator + ov.Value
		report.Applied = append(report.Applied, ov)
	}

	if len(pending) > 0 {
		lines = appendOverrides(lines, pending, knownSections)
	}
	return strings.Join(lines, "\n"), report
}

func appendOverrides(lines []string, pending []Override, knownSections map[string]bool) []string {
	bySection := map[string][]Override{}
	var order []string
	for _, ov := range pending {
		if _, seen := bySection[ov.Section]; !seen {
			order = append(order, ov.Section)
		}
		bySection[ov.Section] = append(bySection[ov.Section], ov)
	}

	for _, section := range order {
		entries := bySection[section]
		if knownSections[section] {
			lines = insertIntoSection(lines, section, entries)
			continue
		}
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "["+section+"]", "")
		for _, ov := range entries {
			lines = append(lines, ov.Name+" = "+ov.Value)
		}
		lines = append(lines, "")
	}
	return lines
}

func insertIntoSection(lines []string, section string, entries []Override) []string {
	start := -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "["+section+"]" {
			start = i
			break
		}
	}
	if start < 0 {
		return lines
	}

	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			end = i
			break
		}
	}

	for end > start+1 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}

	add := make([]string, 0, len(entries))
	for _, ov := range entries {
		add = append(add, ov.Name+" = "+ov.Value)
	}

	out := make([]string, 0, len(lines)+len(add))
	out = append(out, lines[:end]...)
	out = append(out, add...)
	out = append(out, lines[end:]...)
	return out
}
