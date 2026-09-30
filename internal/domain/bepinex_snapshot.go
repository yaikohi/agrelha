package domain

import "time"

type ConfigSnapshot struct {
	Instance    string
	PublishedAt time.Time
	Files       []SnapshotFile
}

type SnapshotFile struct {
	Name     string
	Size     int64
	SHA256   string
	Modified time.Time
}

func (s *ConfigSnapshot) Known() bool { return s != nil }

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

func (s *ConfigSnapshot) Len() int {
	if s == nil {
		return 0
	}
	return len(s.Files)
}
