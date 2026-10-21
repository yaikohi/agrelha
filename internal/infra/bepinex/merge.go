package bepinex

import (
	"os"
	"path/filepath"
	"sort"

	"agrelha/internal/domain"
)

type FileResult struct {
	Name    string
	Changed bool
	Skipped bool
	Report  domain.MergeReport
	Err     error
}

func MergeDir(generatedDir, outDir string, overrides map[string]string) []FileResult {
	if outDir == "" {
		outDir = generatedDir
	}

	names := make([]string, 0, len(overrides))
	for n := range overrides {
		names = append(names, n)
	}
	sort.Strings(names)

	results := make([]FileResult, 0, len(names))
	for _, name := range names {
		res := FileResult{Name: name}

		set := domain.ParseOverrideSet(overrides[name])
		if set.Len() == 0 {
			res.Skipped = true
			results = append(results, res)
			continue
		}

		current, err := os.ReadFile(filepath.Join(generatedDir, name))
		if err != nil && !os.IsNotExist(err) {
			res.Err = err
			results = append(results, res)
			continue
		}

		merged, report := domain.Merge(domain.ParseConfigFile(name, string(current)), set)
		res.Report = report
		if merged == string(current) {
			results = append(results, res)
			continue
		}
		if err := WriteFileAtomic(filepath.Join(outDir, name), []byte(merged)); err != nil {
			res.Err = err
			results = append(results, res)
			continue
		}
		res.Changed = true
		results = append(results, res)
	}
	return results
}

func WriteFileAtomic(path string, body []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".agrelha-merge-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
