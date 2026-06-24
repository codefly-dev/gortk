// Command rtk2json converts rtk TOML filter files into gortk JSON Specs.
//
// This is a build/import-time tool, NOT part of the runtime: gortk's runtime is
// JSON-only and zero-dependency. Run rtk2json to import rtk's (or community)
// .toml filters into specs/*.json once; the TOML dependency lives only here.
//
// Usage:
//
//	rtk2json file1.toml file2.toml ...        # convert listed files
//	rtk2json path/to/rtk/src/filters/*.toml   # glob via the shell
//
// It writes one JSON array of Specs to stdout, validated (every Spec compiles).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mind-build/gortk"
	"github.com/mind-build/gortk/rtkcompat"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "rtk2json:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	exclude := map[string]bool{}
	emitTests := false
	var paths []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--exclude" && i+1 < len(args):
			for n := range strings.SplitSeq(args[i+1], ",") {
				exclude[strings.TrimSpace(n)] = true
			}
			i++
		case args[i] == "--tests":
			emitTests = true
		default:
			paths = append(paths, args[i])
		}
	}
	if len(paths) == 0 {
		return fmt.Errorf("usage: rtk2json [--tests] [--exclude a,b] <file.toml> ... > out.json")
	}
	if emitTests {
		return emitFixtures(paths, exclude)
	}
	var all []gortk.Spec
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		specs, err := rtkcompat.LoadTOML(data)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		for _, s := range specs {
			if exclude[s.Name] {
				continue
			}
			if err := s.Validate(); err != nil {
				return fmt.Errorf("%s: %w", p, err)
			}
			all = append(all, s)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })

	out, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(out, '\n'))
	return err
}

// emitFixtures writes rtk's inline [[tests]] as a JSON fixture array, skipping
// excluded filters.
func emitFixtures(paths []string, exclude map[string]bool) error {
	var all []rtkcompat.Fixture
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fx, err := rtkcompat.LoadTests(data)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		for _, f := range fx {
			if !exclude[f.Filter] {
				all = append(all, f)
			}
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Filter != all[j].Filter {
			return all[i].Filter < all[j].Filter
		}
		return all[i].Name < all[j].Name
	})
	out, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(out, '\n'))
	return err
}
