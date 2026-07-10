// Package rtkcompat loads rtk's TOML filter definitions and converts them to
// native gortk Specs. It lets gortk ingest rtk's built-in filters — and any
// community filters in the same format — verbatim, instead of re-porting them.
//
// This is the ONLY part of gortk that depends on a TOML parser; the core
// package stays dependency-free. Import this package only if you want rtk-TOML
// ingestion.
//
// Field mapping (rtk -> gortk.Spec):
//
//	match_command         -> Match.CommandRegex
//	strip_ansi            -> Lines.StripANSI
//	strip_lines_matching  -> Lines.DropRegexps
//	keep_lines_matching   -> Lines.KeepRegexps
//	truncate_lines_at     -> Lines.TruncateLinesAt
//	max_lines             -> Limit.MaxLines (Keep "tail")
//	head_lines/tail_lines -> Limit.Keep "head"/"tail"
//	on_empty              -> EmptyText
//	match_output          -> MatchOutput
//	filter_stderr         -> Lines.Source ("both" when true, else "stdout")
//	description, replace  -> dropped (replace is unsupported by gortk)
package rtkcompat

import (
	"fmt"
	"sort"

	"github.com/BurntSushi/toml"
	"github.com/codefly-dev/gortk"
)

// tomlDoc is the shape of an rtk filter file: a [filters.<name>] table per
// filter, plus optional [[tests.<name>]] cases that LoadTests can extract.
type tomlDoc struct {
	Filters map[string]rtkFilter `toml:"filters"`
	Tests   map[string][]rtkTest `toml:"tests"`
}

type rtkTest struct {
	Name     string `toml:"name"`
	Input    string `toml:"input"`
	Expected string `toml:"expected"`
}

// Fixture is one rtk test case, flattened with the filter it belongs to. rtk
// compares filter output to Expected with trailing newlines trimmed.
type Fixture struct {
	Filter   string `json:"filter"`
	Name     string `json:"name"`
	Input    string `json:"input"`
	Expected string `json:"expected"`
}

// LoadTests extracts the [[tests.<filter>]] cases from an rtk filter file.
func LoadTests(data []byte) ([]Fixture, error) {
	var doc tomlDoc
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("rtkcompat: parse toml: %w", err)
	}
	names := make([]string, 0, len(doc.Tests))
	for name := range doc.Tests {
		names = append(names, name)
	}
	sort.Strings(names)

	var fixtures []Fixture
	for _, filter := range names {
		for _, tc := range doc.Tests[filter] {
			fixtures = append(fixtures, Fixture{
				Filter:   filter,
				Name:     tc.Name,
				Input:    tc.Input,
				Expected: tc.Expected,
			})
		}
	}
	return fixtures, nil
}

type rtkFilter struct {
	MatchCommand       string         `toml:"match_command"`
	StripANSI          bool           `toml:"strip_ansi"`
	StripLinesMatching []string       `toml:"strip_lines_matching"`
	KeepLinesMatching  []string       `toml:"keep_lines_matching"`
	TruncateLinesAt    int            `toml:"truncate_lines_at"`
	MaxLines           int            `toml:"max_lines"`
	HeadLines          int            `toml:"head_lines"`
	TailLines          int            `toml:"tail_lines"`
	OnEmpty            string         `toml:"on_empty"`
	FilterStderr       bool           `toml:"filter_stderr"`
	MatchOutput        []rtkMatchRule `toml:"match_output"`
}

type rtkMatchRule struct {
	Pattern string `toml:"pattern"`
	Unless  string `toml:"unless"`
	Message string `toml:"message"`
}

// LoadTOML parses an rtk filter file and returns the equivalent gortk Specs,
// sorted by filter name for deterministic ordering. The Specs are returned
// uncompiled — call Spec.Validate (or register them) to surface regex errors.
func LoadTOML(data []byte) ([]gortk.Spec, error) {
	var doc tomlDoc
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("rtkcompat: parse toml: %w", err)
	}
	names := make([]string, 0, len(doc.Filters))
	for name := range doc.Filters {
		names = append(names, name)
	}
	sort.Strings(names)

	specs := make([]gortk.Spec, 0, len(names))
	for _, name := range names {
		specs = append(specs, toSpec(name, doc.Filters[name]))
	}
	return specs, nil
}

// Register converts an rtk filter file and adds every filter to reg. It returns
// the first conversion/compile error encountered (and registers nothing on a
// parse error).
func Register(reg *gortk.Registry, data []byte) error {
	specs, err := LoadTOML(data)
	if err != nil {
		return err
	}
	for _, s := range specs {
		if err := reg.RegisterSpec(s); err != nil {
			return fmt.Errorf("rtkcompat: filter %q: %w", s.Name, err)
		}
	}
	return nil
}

func toSpec(name string, f rtkFilter) gortk.Spec {
	// gortk filters both streams (rtk defaults to stdout-only unless
	// filter_stderr); using "both" is strictly more useful for an agent — error
	// lines on stderr get the same noise-stripping, and the drop patterns are
	// written to target diagnostics either way.
	s := gortk.Spec{
		Name:      name,
		Match:     gortk.MatchSpec{CommandRegex: f.MatchCommand},
		EmptyText: f.OnEmpty,
		Lines: &gortk.LineSpec{
			Source:          "both",
			StripANSI:       f.StripANSI,
			DropRegexps:     f.StripLinesMatching,
			KeepRegexps:     f.KeepLinesMatching,
			TruncateLinesAt: f.TruncateLinesAt,
		},
	}

	if f.MaxLines > 0 || f.HeadLines > 0 || f.TailLines > 0 {
		lim := &gortk.LimitSpec{Keep: "tail"}
		switch {
		case f.HeadLines > 0:
			// rtk's head+tail combo can't be expressed; head wins (keep the top,
			// where the first errors usually are).
			lim.MaxLines = f.HeadLines
			lim.Keep = "head"
		case f.TailLines > 0:
			lim.MaxLines = f.TailLines
		default:
			lim.MaxLines = f.MaxLines
		}
		s.Limit = lim
	}

	for _, m := range f.MatchOutput {
		s.MatchOutput = append(s.MatchOutput, gortk.OutputRule{
			Pattern: m.Pattern,
			Unless:  m.Unless,
			Message: m.Message,
		})
	}
	return s
}
