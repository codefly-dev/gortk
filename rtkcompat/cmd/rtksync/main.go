// Command rtksync imports rtk's entire TOML filter catalog into specs/rtk.json.
//
// It is the canonical, Go-native generator for specs/rtk.json: rather than
// hand-editing that file, run rtksync to (optionally) clone upstream rtk and
// convert every TOML filter deterministically via the rtkcompat package. The
// runtime stays JSON-only and zero-dependency; this tool — and its TOML
// dependency — lives in the rtkcompat module.
//
// Usage:
//
//	# clone rtk and regenerate ../specs/rtk.json (run from the rtkcompat dir):
//	go run ./cmd/rtksync
//
//	# use an existing checkout, pin a release, or just check for drift:
//	go run ./cmd/rtksync -dir /path/to/rtk
//	go run ./cmd/rtksync -ref v0.42.4
//	go run ./cmd/rtksync -check          # exit non-zero if specs/rtk.json is stale
//
// Flags:
//
//	-out      output path           (default ../specs/rtk.json)
//	-repo     rtk git URL           (default https://github.com/codefly-dev/rtk)
//	-ref      git ref/tag to clone  (default: repo default branch)
//	-dir      existing rtk checkout  (skips cloning)
//	-glob     filter glob in rtk    (default src/filters/*.toml)
//	-exclude  comma-separated filter names to skip
//	-check    diff instead of write; non-zero exit on drift
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/codefly-dev/gortk"
	"github.com/codefly-dev/gortk/rtkcompat"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "rtksync:", err)
		os.Exit(1)
	}
}

func run() error {
	out := flag.String("out", filepath.Join("..", "specs", "rtk.json"), "output path for the generated specs")
	repo := flag.String("repo", "https://github.com/codefly-dev/rtk", "rtk git URL")
	ref := flag.String("ref", "", "git ref/tag to clone (default: the repo's default branch)")
	dir := flag.String("dir", "", "existing rtk checkout (skips cloning)")
	glob := flag.String("glob", filepath.Join("src", "filters", "*.toml"), "filter glob within the rtk repo")
	excludeCSV := flag.String("exclude", "", "comma-separated filter names to skip")
	check := flag.Bool("check", false, "diff against the existing file and exit non-zero on drift")
	flag.Parse()

	exclude := map[string]bool{}
	for n := range strings.SplitSeq(*excludeCSV, ",") {
		if n = strings.TrimSpace(n); n != "" {
			exclude[n] = true
		}
	}

	// Filters gortk curates natively (the sibling spec files next to -out)
	// always win over the upstream import: a name defined in build.json,
	// js.json, … is skipped here so the catalog never carries duplicates,
	// and -check regenerates with the exact same rule.
	native, err := siblingSpecNames(*out)
	if err != nil {
		return err
	}
	for n := range native {
		exclude[n] = true
	}

	rtkDir := *dir
	if rtkDir == "" {
		tmp, err := os.MkdirTemp("", "rtksync-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		fmt.Fprintf(os.Stderr, "cloning %s@%s ...\n", *repo, *ref)
		if err := gitClone(*repo, *ref, tmp); err != nil {
			return fmt.Errorf("clone rtk: %w", err)
		}
		rtkDir = tmp
	}

	files, err := filepath.Glob(filepath.Join(rtkDir, *glob))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no TOML filters matched %s (rtk may have moved them; set -glob)",
			filepath.Join(rtkDir, *glob))
	}

	generated, err := convert(files, exclude)
	if err != nil {
		return err
	}

	if *check {
		existing, err := os.ReadFile(*out)
		if err != nil {
			return fmt.Errorf("read %s: %w", *out, err)
		}
		if !bytes.Equal(normalizeEOL(existing), normalizeEOL(generated)) {
			fmt.Fprintf(os.Stderr, "%s is OUT OF DATE with rtk@%s — run rtksync and commit.\n", *out, *ref)
			return fmt.Errorf("drift detected")
		}
		fmt.Fprintf(os.Stderr, "%s is in sync with rtk@%s\n", *out, *ref)
		return nil
	}

	if err := os.WriteFile(*out, generated, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s from rtk@%s (%d files)\n", *out, *ref, len(files))
	return nil
}

// convert reads every TOML filter file and returns the pretty-printed,
// name-sorted JSON spec array — the exact bytes specs/rtk.json should contain.
func convert(files []string, exclude map[string]bool) ([]byte, error) {
	var all []gortk.Spec
	for _, p := range files {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		specs, err := rtkcompat.LoadTOML(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		for _, s := range specs {
			if exclude[s.Name] {
				continue
			}
			if err := s.Validate(); err != nil {
				return nil, fmt.Errorf("%s: filter %q: %w", p, s.Name, err)
			}
			all = append(all, s)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })

	buf, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(buf, '\n'), nil
}

// siblingSpecNames collects every filter name defined by the curated spec
// files that live next to outPath (skipping outPath itself). These names are
// excluded from the upstream import so native specs always take precedence.
func siblingSpecNames(outPath string) (map[string]bool, error) {
	siblings, err := filepath.Glob(filepath.Join(filepath.Dir(outPath), "*.json"))
	if err != nil {
		return nil, err
	}
	outAbs, err := filepath.Abs(outPath)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, p := range siblings {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		if abs == outAbs {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var specs []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(data, &specs); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		for _, s := range specs {
			if s.Name != "" {
				names[s.Name] = true
			}
		}
	}
	return names, nil
}

func gitClone(url, ref, dst string) error {
	args := []string{"clone", "--depth", "1"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, url, dst)
	cmd := exec.Command("git", args...)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	return cmd.Run()
}

// normalizeEOL strips CR so a drift check doesn't trip on line-ending diffs.
func normalizeEOL(b []byte) []byte { return bytes.ReplaceAll(b, []byte("\r"), nil) }
