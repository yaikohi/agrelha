package backups

import (
	"os"
	"time"
)

type Info struct {
	Count      int
	TotalSize  int64
	LatestName string
	LatestSize int64
	LatestAt   time.Time
}

func Stat(dir string) (Info, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Info{}, err
	}
	var info Info
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		info.Count++
		info.TotalSize += fi.Size()
		if fi.ModTime().After(info.LatestAt) {
			info.LatestAt = fi.ModTime()
			info.LatestName = e.Name()
			info.LatestSize = fi.Size()
		}
	}
	return info, nil
}
