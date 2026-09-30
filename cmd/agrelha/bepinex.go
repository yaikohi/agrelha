// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"agrelha/internal/domain"
)

func runBepInEx(stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: agrelha bepinex merge [flags]")
		return 2
	}
	switch args[0] {
	case "merge":
		return runBepInExMerge(stdout, stderr, args[1:])
	default:
		fmt.Fprintf(stderr, "unknown bepinex subcommand %q\n", args[0])
		return 2
	}
}

func runBepInExMerge(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("bepinex merge", flag.ContinueOnError)
	fs.SetOutput(stderr)
	generated := fs.String("generated", "", "directory holding the mod-generated .cfg files")
	overrides := fs.String("overrides", "", "directory holding the operator's override sets")
	out := fs.String("out", "", "directory to write the merged files to (defaults to -generated)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *generated == "" || *overrides == "" {
		fmt.Fprintln(stderr, "both -generated and -overrides are required")
		return 2
	}
	if *out == "" {
		*out = *generated
	}

	names, err := overrideFileNames(*overrides)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(stdout, ">> bepinex merge: no overrides at %s, nothing to do\n", *overrides)
			return 0
		}
		fmt.Fprintf(stderr, ">> bepinex merge: cannot read %s: %v\n", *overrides, err)
		return 1
	}
	if len(names) == 0 {
		fmt.Fprintf(stdout, ">> bepinex merge: no override files, nothing to do\n")
		return 0
	}

	failed := false
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(*overrides, name))
		if err != nil {
			fmt.Fprintf(stderr, ">> bepinex merge: %s: cannot read override: %v\n", name, err)
			failed = true
			continue
		}
		set := domain.ParseOverrideSet(string(body))
		if set.Len() == 0 {
			fmt.Fprintf(stdout, ">> bepinex merge: %s: override set is empty, leaving the file alone\n", name)
			continue
		}

		target := filepath.Join(*out, name)
		current, err := os.ReadFile(filepath.Join(*generated, name))
		if err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(stderr, ">> bepinex merge: %s: cannot read generated config: %v\n", name, err)
			failed = true
			continue
		}

		merged, report := domain.Merge(domain.ParseConfigFile(name, string(current)), set)
		if merged == string(current) {
			fmt.Fprintf(stdout, ">> bepinex merge: %s: already up to date (%d overrides)\n", name, set.Len())
			continue
		}
		if err := writeFileAtomic(target, []byte(merged)); err != nil {
			fmt.Fprintf(stderr, ">> bepinex merge: %s: cannot write: %v\n", name, err)
			failed = true
			continue
		}
		fmt.Fprintf(stdout, ">> bepinex merge: %s: %d applied, %d appended, %d unmatched, %d already set\n",
			name, len(report.Applied), len(report.Appended), len(report.Stale), len(report.NoOp))
		for _, ov := range report.Stale {
			fmt.Fprintf(stdout, ">> bepinex merge: %s: %s is not declared by the mod; written anyway\n", name, ov.Key())
		}
	}
	if failed {
		return 1
	}
	return 0
}

func overrideFileNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.IsDir() {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}

func writeFileAtomic(path string, body []byte) error {
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
