package gortk

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestRTKFixtures runs rtk's own [[tests]] cases (extracted by cmd/rtk2json
// --tests) against the converted Specs. This is the faithfulness check for the
// rtk-imported catalog: rtk feeds `input` to the filter and expects `expected`,
// compared with trailing newlines trimmed (matching rtk's own comparison).
//
// Filters are invoked directly by name (not via command routing) because rtk
// tests the filter, not a command line.
func TestRTKFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/rtk_fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Filter   string `json:"filter"`
		Name     string `json:"name"`
		Input    string `json:"input"`
		Expected string `json:"expected"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}

	// Index specs by name and compile once.
	byName := map[string]Filter{}
	for _, s := range DefaultSpecs() {
		if f, err := s.Compile(); err == nil {
			byName[s.Name] = f
		}
	}

	for _, fx := range fixtures {
		t.Run(fx.Filter+"/"+fx.Name, func(t *testing.T) {
			f, ok := byName[fx.Filter]
			if !ok {
				t.Skipf("filter %q not in catalog", fx.Filter)
			}
			// rtk feeds the case input as the command's stdout.
			res := f.Apply(Command{Name: fx.Filter, Stdout: []byte(fx.Input)})
			got := strings.TrimRight(res.Text, "\n")
			want := strings.TrimRight(fx.Expected, "\n")
			if got != want {
				t.Errorf("mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}
