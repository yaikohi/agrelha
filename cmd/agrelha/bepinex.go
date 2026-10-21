// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"agrelha/internal/infra/bepinex"
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
	overrides_ := fs.String("overrides", "", "directory holding the operator's override sets")
	out := fs.String("out", "", "directory to write the merged files to (defaults to -generated)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *generated == "" || *overrides_ == "" {
		fmt.Fprintln(stderr, "both -generated and -overrides are required")
		return 2
	}
	if *out == "" {
		*out = *generated
	}

	names, err := overrideFileNames(*overrides_)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(stdout, ">> bepinex merge: no overrides at %s, nothing to do\n", *overrides_)
			return 0
		}
		fmt.Fprintf(stderr, ">> bepinex merge: cannot read %s: %v\n", *overrides_, err)
		return 1
	}
	if len(names) == 0 {
		fmt.Fprintf(stdout, ">> bepinex merge: no override files, nothing to do\n")
		return 0
	}

	overrides := make(map[string]string, len(names))
	failed := false
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(*overrides_, name))
		if err != nil {
			fmt.Fprintf(stderr, ">> bepinex merge: %s: cannot read override: %v\n", name, err)
			failed = true
			continue
		}
		overrides[name] = string(body)
	}

	for _, res := range bepinex.MergeDir(*generated, *out, overrides) {
		switch {
		case res.Err != nil:
			fmt.Fprintf(stderr, ">> bepinex merge: %s: %v\n", res.Name, res.Err)
			failed = true
		case res.Skipped:
			fmt.Fprintf(stdout, ">> bepinex merge: %s: override set is empty, leaving the file alone\n", res.Name)
		case !res.Changed:
			fmt.Fprintf(stdout, ">> bepinex merge: %s: already up to date\n", res.Name)
		default:
			fmt.Fprintf(stdout, ">> bepinex merge: %s: %d applied, %d appended, %d unmatched, %d already set\n",
				res.Name, len(res.Report.Applied), len(res.Report.Appended), len(res.Report.Stale), len(res.Report.NoOp))
			for _, ov := range res.Report.Stale {
				fmt.Fprintf(stdout, ">> bepinex merge: %s: %s is not declared by the mod; written anyway\n", res.Name, ov.Key())
			}
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
