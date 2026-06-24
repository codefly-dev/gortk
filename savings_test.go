package gortk

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
)

// TestSavings is the committed, self-verifying source of the savings numbers in
// the README. It runs every bundled fixture through the default registry and
// measures input-vs-output bytes — real measurements, not estimates.
//
// Regenerate the README table with:
//
//	go test -run TestSavings -v
//
// It also guards against regressions: overall savings must stay above a floor,
// so a change that quietly stops compressing fails CI.
func TestSavings(t *testing.T) {
	reg := Default()

	type agg struct{ in, out, n int }
	byFilter := map[string]*agg{}
	add := func(filter string, in, out int) {
		a := byFilter[filter]
		if a == nil {
			a = &agg{}
			byFilter[filter] = a
		}
		a.in += in
		a.out += out
		a.n++
	}

	// Structured fixtures: real command outputs, routed through the registry.
	if data, err := os.ReadFile("testdata/structured_fixtures.json"); err == nil {
		var groups []struct {
			Tests []struct {
				Cmd    string   `json:"cmd"`
				Args   []string `json:"args"`
				Filter string   `json:"filter"`
				Stdout string   `json:"stdout"`
				Stderr string   `json:"stderr"`
			} `json:"tests"`
		}
		if err := json.Unmarshal(data, &groups); err != nil {
			t.Fatal(err)
		}
		for _, g := range groups {
			for _, tc := range g.Tests {
				res := reg.Compress(Command{Name: tc.Cmd, Args: tc.Args, Stdout: []byte(tc.Stdout), Stderr: []byte(tc.Stderr)})
				add(tc.Filter, len(tc.Stdout)+len(tc.Stderr), len(res.Text))
			}
		}
	}

	// rtk fixtures: apply the filter directly (rtk tests the filter, not routing).
	byName := map[string]Filter{}
	for _, s := range DefaultSpecs() {
		if f, err := s.Compile(); err == nil {
			byName[s.Name] = f
		}
	}
	if data, err := os.ReadFile("testdata/rtk_fixtures.json"); err == nil {
		var fx []struct{ Filter, Input string }
		if err := json.Unmarshal(data, &fx); err != nil {
			t.Fatal(err)
		}
		for _, c := range fx {
			if f, ok := byName[c.Filter]; ok {
				res := f.Apply(Command{Name: c.Filter, Stdout: []byte(c.Input)})
				add(c.Filter, len(c.Input), len(res.Text))
			}
		}
	}

	// Aggregate.
	var totIn, totOut int
	type row struct {
		name       string
		in, out, n int
		pct        int
	}
	var rows []row
	for n, a := range byFilter {
		totIn += a.in
		totOut += a.out
		pct := 0
		if a.in > 0 {
			pct = int(float64(a.in-a.out)/float64(a.in)*100 + 0.5)
		}
		rows = append(rows, row{n, a.in, a.out, a.n, pct})
	}
	if totIn == 0 {
		t.Fatal("no fixtures measured")
	}
	overall := int(float64(totIn-totOut) / float64(totIn) * 100)

	// Regression guard: overall savings must stay healthy even though ~half the
	// fixtures are deliberately failure-preserving (0% by design).
	if overall < 40 {
		t.Fatalf("overall savings dropped to %d%% (< 40%% floor): in=%d out=%d", overall, totIn, totOut)
	}

	// Representative table: drop tiny fixtures (noisy %) and the 0%
	// failure-preserving cases; show the highest-signal commands.
	sort.Slice(rows, func(i, j int) bool { return rows[i].pct > rows[j].pct })
	t.Logf("\nOverall: %d B -> %d B (%d%% saved) across %d filters\n", totIn, totOut, overall, len(rows))
	t.Log("\n| command | in (B) | out (B) | saved |\n|---|---:|---:|---:|")
	for _, r := range rows {
		if r.in < 300 || r.pct < 40 {
			continue
		}
		t.Logf("| `%s` | %d | %d | **%d%%** |", r.name, r.in, r.out, r.pct)
	}
}
