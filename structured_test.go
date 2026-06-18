package gortk

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The structured commands (rspec, aws, psql, grep, …) were ported from rtk's
// Rust parsers, which carry no portable fixtures, so each port shipped its own.
// This runs those fixtures against the live Default() registry — both to verify
// the specs and to catch drift if they're edited.

type fixtureFile struct {
	Name  string `json:"name"`
	Tests []struct {
		Name       string   `json:"name"`
		Cmd        string   `json:"cmd"`
		Args       []string `json:"args"`
		Stdout     string   `json:"stdout"`
		Stderr     string   `json:"stderr"`
		Filter     string   `json:"filter"`
		WantHas    []string `json:"wantHas"`
		WantNotHas []string `json:"wantNotHas"`
	} `json:"tests"`
}

func TestStructuredFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/structured_fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var files []fixtureFile
	if err := json.Unmarshal(data, &files); err != nil {
		t.Fatal(err)
	}
	reg := Default()

	for _, f := range files {
		for _, tc := range f.Tests {
			t.Run(f.Name+"/"+tc.Name, func(t *testing.T) {
				res := reg.Compress(Command{
					Name: tc.Cmd, Args: tc.Args,
					Stdout: []byte(tc.Stdout), Stderr: []byte(tc.Stderr),
				})
				if tc.Filter != "" && res.Filter != tc.Filter {
					t.Errorf("filter = %q, want %q\n--- output ---\n%s", res.Filter, tc.Filter, res.Text)
					return
				}
				for _, w := range tc.WantHas {
					if w != "" && !strings.Contains(res.Text, w) {
						t.Errorf("missing %q in:\n%s", w, res.Text)
					}
				}
				for _, n := range tc.WantNotHas {
					if n != "" && strings.Contains(res.Text, n) {
						t.Errorf("unexpected %q in:\n%s", n, res.Text)
					}
				}
			})
		}
	}
}
