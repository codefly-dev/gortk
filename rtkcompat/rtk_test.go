package rtkcompat

import (
	"strings"
	"testing"

	"github.com/mind-build/gortk"
)

const sampleTOML = `
schema_version = 1

[filters.my-tool]
description = "Compact my-tool output"
match_command = "^my-tool\\s+build"
strip_ansi = true
strip_lines_matching = ["^\\s*$", "^Downloading", "^Installing"]
truncate_lines_at = 120
max_lines = 30
on_empty = "my-tool: ok"

[filters.deployer]
match_command = "^deploy\\b"
match_output = [
  { pattern = "Deployment complete", unless = "error", message = "deploy: ok" },
]

[[tests.my-tool]]
name = "ignored by loader"
input = "x"
expected = "x"
`

func TestLoadTOMLMapsFields(t *testing.T) {
	specs, err := LoadTOML([]byte(sampleTOML))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("got %d specs, want 2", len(specs))
	}
	// Sorted by name: deployer, my-tool.
	dep, mt := specs[0], specs[1]
	if dep.Name != "deployer" || mt.Name != "my-tool" {
		t.Fatalf("names/order wrong: %q %q", dep.Name, mt.Name)
	}

	if mt.Match.CommandRegex != "^my-tool\\s+build" {
		t.Errorf("match_command not mapped: %q", mt.Match.CommandRegex)
	}
	if mt.Lines == nil || !mt.Lines.StripANSI {
		t.Error("strip_ansi not mapped")
	}
	if mt.Lines.Source != "both" {
		t.Errorf("source = %q, want both", mt.Lines.Source)
	}
	if len(mt.Lines.DropRegexps) != 3 {
		t.Errorf("strip_lines_matching -> drop_regexps wrong: %v", mt.Lines.DropRegexps)
	}
	if mt.Lines.TruncateLinesAt != 120 {
		t.Errorf("truncate_lines_at = %d", mt.Lines.TruncateLinesAt)
	}
	if mt.Limit == nil || mt.Limit.MaxLines != 30 {
		t.Errorf("max_lines not mapped: %+v", mt.Limit)
	}
	if mt.EmptyText != "my-tool: ok" {
		t.Errorf("on_empty -> empty_text wrong: %q", mt.EmptyText)
	}

	if len(dep.MatchOutput) != 1 || dep.MatchOutput[0].Message != "deploy: ok" || dep.MatchOutput[0].Unless != "error" {
		t.Errorf("match_output not mapped: %+v", dep.MatchOutput)
	}

	// Converted specs must be valid gortk specs.
	for _, s := range specs {
		if err := s.Validate(); err != nil {
			t.Errorf("converted spec %q invalid: %v", s.Name, err)
		}
	}
}

func TestConvertedSpecCompressesEndToEnd(t *testing.T) {
	specs, err := LoadTOML([]byte(sampleTOML))
	if err != nil {
		t.Fatal(err)
	}
	reg := gortk.New()
	for _, s := range specs {
		if err := reg.RegisterSpec(s); err != nil {
			t.Fatal(err)
		}
	}
	// my-tool build: download/install noise stripped, on_empty fires when empty.
	res := reg.Compress(gortk.Command{
		Name: "my-tool", Args: []string{"build"},
		Stdout: []byte("Downloading x\nInstalling y\n"),
	})
	if res.Text != "my-tool: ok\n" {
		t.Errorf("expected on_empty collapse, got %q", res.Text)
	}
	// deployer: match_output collapse.
	res = reg.Compress(gortk.Command{
		Name: "deploy", Stdout: []byte("step 1\nDeployment complete\n"),
	})
	if !strings.Contains(res.Text, "deploy: ok") {
		t.Errorf("match_output collapse failed: %q", res.Text)
	}
}

func TestRegisterHelper(t *testing.T) {
	reg := gortk.New()
	if err := Register(reg, []byte(sampleTOML)); err != nil {
		t.Fatal(err)
	}
	res := reg.Compress(gortk.Command{Name: "deploy", Stdout: []byte("Deployment complete\n")})
	if !strings.Contains(res.Text, "deploy: ok") {
		t.Errorf("registered rtk filter did not apply: %q", res.Text)
	}
}
