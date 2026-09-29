package domain

import "time"

// ConfigSnapshot is the copy of an Instance's Generated configs that the
// publish sidecar leaves on the backups export. It is agrelha's only way to see
// them: agrelha has no pods/exec in the valheim namespace, and the files live
// on a node-local PVC it cannot reach.
//
// A missing snapshot means "not published yet", which the UI must be able to
// tell apart from "this world has no configs" - the same rule ServerBuild
// follows for an unanswered Steam query.
type ConfigSnapshot struct {
	Instance    string
	PublishedAt time.Time
	Files       []SnapshotFile
}

// SnapshotFile is one Generated config as the sidecar found it. SHA256 is what
// makes a stale edit detectable: BepInEx rewrites these files on its own
// schedule, and an Override written against a file that has since moved would
// land on the wrong Setting.
type SnapshotFile struct {
	Name     string
	Size     int64
	SHA256   string
	Modified time.Time
}

// Known reports whether a snapshot has been published at all. Nil-safe, so
// callers can pass a reader's result straight through.
func (s *ConfigSnapshot) Known() bool { return s != nil }

// Lookup finds one file by name.
func (s *ConfigSnapshot) Lookup(name string) (SnapshotFile, bool) {
	if s == nil {
		return SnapshotFile{}, false
	}
	for _, f := range s.Files {
		if f.Name == name {
			return f, true
		}
	}
	return SnapshotFile{}, false
}

// Len reports how many Generated configs the snapshot holds.
func (s *ConfigSnapshot) Len() int {
	if s == nil {
		return 0
	}
	return len(s.Files)
}
