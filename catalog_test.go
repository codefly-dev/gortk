package gortk

import (
	"strings"
	"testing"
)

// --- new engine features ----------------------------------------------------

func TestCommandRegexMatch(t *testing.T) {
	f := compile(t, Spec{
		Name: "t", Match: MatchSpec{CommandRegex: "^uv\\s+(sync|pip\\s+install)\\b"},
		Lines: &LineSpec{},
	})
	cases := []struct {
		name  string
		args  []string
		match bool
	}{
		{"uv", []string{"sync"}, true},
		{"uv", []string{"sync", "--frozen"}, true},
		{"uv", []string{"pip", "install", "requests"}, true},
		{"uv", []string{"run", "pytest"}, false},
		{"/usr/bin/uv", []string{"sync"}, true},
		{"go", []string{"build"}, false},
	}
	for _, tc := range cases {
		if got := f.Match(tc.name, tc.args); got != tc.match {
			t.Errorf("Match(%q, %v) = %v, want %v", tc.name, tc.args, got, tc.match)
		}
	}
}

func TestTruncateLinesAt(t *testing.T) {
	f := compile(t, Spec{
		Name: "t", Match: MatchSpec{Command: "x"},
		Lines: &LineSpec{TruncateLinesAt: 10},
	})
	res := f.Apply(Command{Stdout: []byte("short\nthis-line-is-definitely-too-long\n")})
	if !strings.Contains(res.Text, "short\n") {
		t.Errorf("short line altered: %q", res.Text)
	}
	if !strings.Contains(res.Text, "this-line-…") {
		t.Errorf("long line not truncated with ellipsis: %q", res.Text)
	}
}

func TestJSONRootArray(t *testing.T) {
	// ruff/eslint shape: a bare top-level JSON array.
	f := compile(t, Spec{
		Name: "t", Match: MatchSpec{Command: "ruff"},
		JSON: &JSONSpec{ArrayField: "", ItemTemplate: "{filename}:{location.row}:{location.column} {code}", SummaryTemplate: "{count} issues"},
	})
	in := `[{"code":"F401","filename":"a.py","location":{"row":3,"column":1}},{"code":"E501","filename":"b.py","location":{"row":9,"column":80}}]`
	res := f.Apply(Command{Stdout: []byte(in)})
	if !strings.Contains(res.Text, "a.py:3:1 F401") || !strings.Contains(res.Text, "b.py:9:80 E501") {
		t.Errorf("root-array json render wrong:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "2 issues") {
		t.Errorf("count wrong:\n%s", res.Text)
	}
}

// --- ported catalog: behavior checks via Default() --------------------------
//
// Fixtures mirror rtk's own filter tests where applicable, so these double as
// parity checks against the upstream catalog.

func TestCatalogBehaviors(t *testing.T) {
	reg := Default()
	cases := []struct {
		name       string
		cmd        string
		args       []string
		stdout     string
		stderr     string
		filter     string
		wantHas    []string // substrings that must be present
		wantNotHas []string // substrings that must be absent
	}{
		{
			name: "make collapses to ok when all stripped",
			cmd:  "make", args: []string{"build"},
			stderr: "make[1]: Entering directory '/x'\nmake[1]: Leaving directory '/x'\n",
			filter: "make", wantHas: []string{"make: ok"},
		},
		{
			name: "make keeps real output",
			cmd:  "make", args: []string{},
			stdout: "make[1]: Entering directory '/x'\ngcc -O2 foo.c\nmake[1]: Leaving directory '/x'\n",
			filter: "make", wantHas: []string{"gcc -O2 foo.c"}, wantNotHas: []string{"Entering directory"},
		},
		{
			name: "uv sync audited short-circuits",
			cmd:  "uv", args: []string{"sync"},
			stdout: "Resolved 42 packages in 123ms\nAudited 42 packages in 0.05ms\n",
			filter: "uv-sync", wantHas: []string{"up to date"},
		},
		{
			name: "go build ok on empty",
			cmd:  "go", args: []string{"build", "./..."},
			filter: "go-build", wantHas: []string{"go build: ok"},
		},
		{
			name: "go build keeps compiler errors",
			cmd:  "go", args: []string{"build", "./..."},
			stderr: "go: downloading example.com/x v1.0.0\n./main.go:10:2: undefined: foo\n",
			filter: "go-build", wantHas: []string{"main.go:10:2: undefined: foo"}, wantNotHas: []string{"downloading"},
		},
		{
			name: "npm install collapses on success",
			cmd:  "npm", args: []string{"install"},
			stdout: "added 50 packages, and audited 51 packages in 3s\n\n5 packages are looking for funding\n  run `npm fund` for details\n\nfound 0 vulnerabilities\n",
			filter: "npm-install", wantHas: []string{"npm install: ok"},
		},
		{
			name: "npm install does not hide errors",
			cmd:  "npm", args: []string{"install"},
			stderr: "npm error code ERESOLVE\nnpm error ERESOLVE unable to resolve dependency tree\n",
			filter: "npm-install", wantHas: []string{"ERESOLVE"}, wantNotHas: []string{"npm install: ok"},
		},
		{
			name: "ruff json root array",
			cmd:  "ruff", args: []string{"check", "--output-format", "json", "."},
			stdout: `[{"code":"F401","message":"unused import","filename":"a.py","location":{"row":1,"column":1}}]`,
			filter: "ruff", wantHas: []string{"a.py:1:1 F401 unused import"},
		},
		{
			name: "ruff text all-clear",
			cmd:  "ruff", args: []string{"check", "."},
			stdout: "All checks passed!\n",
			filter: "ruff", wantHas: []string{"ruff: ok"},
		},
		{
			name: "tsc ok on empty",
			cmd:  "tsc", args: []string{"--noEmit"},
			filter: "tsc", wantHas: []string{"tsc: ok"},
		},
		{
			name: "mypy success collapses",
			cmd:  "mypy", args: []string{"."},
			stdout: "Success: no issues found in 12 source files\n",
			filter: "mypy", wantHas: []string{"mypy: ok"},
		},
		{
			name: "pytest green collapses",
			cmd:  "pytest", args: []string{},
			stdout: "============ test session starts ============\ncollected 3 items\ntests/a.py ... [100%]\n=================== 3 passed in 0.12s ===================\n",
			filter: "pytest", wantHas: []string{"pytest: ok"},
		},
		{
			name: "python -m pytest matches",
			cmd:  "python", args: []string{"-m", "pytest"},
			stdout: "=================== 5 passed in 0.3s ===================\n",
			filter: "pytest", wantHas: []string{"pytest: ok"},
		},
		{
			name: "turbo strips cache noise",
			cmd:  "turbo", args: []string{"run", "build"},
			stdout: "• Packages in scope: web\ncache hit, replaying logs\nweb:build: done\nTasks:    1 successful\nDuration: 1.2s\n",
			filter: "turbo", wantHas: []string{"web:build: done"}, wantNotHas: []string{"cache hit", "Duration:"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := reg.Compress(Command{
				Name: tc.cmd, Args: tc.args,
				Stdout: []byte(tc.stdout), Stderr: []byte(tc.stderr),
			})
			if tc.filter != "" && res.Filter != tc.filter {
				t.Fatalf("filter = %q, want %q\noutput:\n%s", res.Filter, tc.filter, res.Text)
			}
			for _, want := range tc.wantHas {
				if !strings.Contains(res.Text, want) {
					t.Errorf("missing %q in:\n%s", want, res.Text)
				}
			}
			for _, no := range tc.wantNotHas {
				if strings.Contains(res.Text, no) {
					t.Errorf("should not contain %q in:\n%s", no, res.Text)
				}
			}
		})
	}
}

// --- catalog hygiene --------------------------------------------------------

func TestNoDuplicateFilterNames(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range DefaultSpecs() {
		if seen[s.Name] {
			t.Errorf("duplicate filter name %q", s.Name)
		}
		seen[s.Name] = true
	}
}

func TestGoTestNotShadowedBySpec(t *testing.T) {
	// `go test` must hit the structured code filter, not go-build/go-run specs.
	res := Default().Compress(Command{
		Name: "go", Args: []string{"test", "./..."},
		Stdout: []byte("ok  \texample/p\t0.01s\n"),
	})
	if res.Filter != "go-test" {
		t.Errorf("go test routed to %q, want go-test", res.Filter)
	}
}
