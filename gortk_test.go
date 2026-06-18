package gortk

import (
	"strings"
	"testing"
)

func TestGoTestJSONKeepsFailuresDropsPasses(t *testing.T) {
	stdout := strings.Join([]string{
		`{"Action":"run","Package":"p","Test":"TestA"}`,
		`{"Action":"output","Package":"p","Test":"TestA","Output":"ok detail\n"}`,
		`{"Action":"pass","Package":"p","Test":"TestA"}`,
		`{"Action":"run","Package":"p","Test":"TestB"}`,
		`{"Action":"output","Package":"p","Test":"TestB","Output":"    foo_test.go:12: want 3 got 5\n"}`,
		`{"Action":"fail","Package":"p","Test":"TestB"}`,
		`{"Action":"skip","Package":"p","Test":"TestC"}`,
	}, "\n")

	res := Default().Compress(Command{
		Name: "go", Args: []string{"test", "-json", "./..."},
		Stdout: []byte(stdout), ExitCode: 1,
	})

	if res.Filter != "go-test" {
		t.Fatalf("filter = %q, want go-test", res.Filter)
	}
	if !strings.Contains(res.Text, "foo_test.go:12: want 3 got 5") {
		t.Errorf("failure detail missing:\n%s", res.Text)
	}
	if strings.Contains(res.Text, "ok detail") {
		t.Errorf("passing-test output should have been dropped:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "1 passed, 1 failed, 1 skipped") {
		t.Errorf("summary line wrong:\n%s", res.Text)
	}
	if res.Lossless() {
		t.Errorf("expected Truncation.Happened to be true (passes were dropped)")
	}
}

func TestGoTestTextFallback(t *testing.T) {
	stdout := strings.Join([]string{
		"=== RUN   TestA",
		"--- PASS: TestA (0.00s)",
		"ok  	example/p	0.012s",
		"--- FAIL: TestB (0.00s)",
		"    b_test.go:9: boom",
		"FAIL",
	}, "\n")

	res := Default().Compress(Command{
		Name: "go", Args: []string{"test", "./..."},
		Stdout: []byte(stdout), ExitCode: 1,
	})
	if !strings.Contains(res.Text, "FAIL: TestB") || !strings.Contains(res.Text, "b_test.go:9: boom") {
		t.Errorf("failure not preserved:\n%s", res.Text)
	}
	if strings.Contains(res.Text, "=== RUN") || strings.Contains(res.Text, "ok  ") {
		t.Errorf("noise not dropped:\n%s", res.Text)
	}
}

func TestGolangciLintJSON(t *testing.T) {
	stdout := `{"Issues":[{"FromLinter":"errcheck","Text":"unchecked error","Pos":{"Filename":"x.go","Line":4,"Column":2}}]}`
	res := Default().Compress(Command{
		Name: "golangci-lint", Args: []string{"run", "--out-format", "json"},
		Stdout: []byte(stdout), ExitCode: 1,
	})
	if res.Filter != "golangci-lint" {
		t.Fatalf("filter = %q", res.Filter)
	}
	if !strings.Contains(res.Text, "x.go:4:2 [errcheck] unchecked error") {
		t.Errorf("issue not formatted:\n%s", res.Text)
	}
}

func TestGitStatusDropsHints(t *testing.T) {
	stdout := strings.Join([]string{
		"On branch main",
		"Your branch is up to date with 'origin/main'.",
		"Changes not staged for commit:",
		`  (use "git add <file>..." to update what will be committed)`,
		"	modified:   a.go",
		"",
	}, "\n")
	res := Default().Compress(Command{
		Name: "git", Args: []string{"status"}, Stdout: []byte(stdout),
	})
	if res.Filter != "git-status" {
		t.Fatalf("filter = %q", res.Filter)
	}
	if !strings.Contains(res.Text, "modified:   a.go") {
		t.Errorf("change line dropped:\n%s", res.Text)
	}
	if strings.Contains(res.Text, "On branch") || strings.Contains(res.Text, "(use ") {
		t.Errorf("hints/header not dropped:\n%s", res.Text)
	}
}

func TestPassthroughIsLossless(t *testing.T) {
	res := Default().Compress(Command{
		Name: "frobnicate-xyz", Args: []string{"build"},
		Stdout: []byte("line1\nline2\n"), Stderr: []byte("warn\n"),
	})
	if res.Filter != "passthrough" {
		t.Fatalf("filter = %q, want passthrough", res.Filter)
	}
	if !res.Lossless() {
		t.Errorf("passthrough should be lossless")
	}
	if !strings.Contains(res.Text, "line1") || !strings.Contains(res.Text, "warn") {
		t.Errorf("passthrough dropped content:\n%s", res.Text)
	}
}

func TestBoundKeepsTail(t *testing.T) {
	big := strings.Repeat("x", 100) + "TAIL\n"
	r := Default().WithMaxBytes(10)
	res := r.Compress(Command{Name: "cat", Stdout: []byte(big)})
	if !strings.Contains(res.Text, "TAIL") {
		t.Errorf("tail not preserved after bounding:\n%s", res.Text)
	}
	if res.Lossless() {
		t.Errorf("bounding should mark truncation")
	}
}
