package gortk

import (
	"context"
	"strings"
	"testing"
)

func TestExecRunnerCapturesOutput(t *testing.T) {
	cmd, err := ExecRunner{}.Run(context.Background(), Invocation{
		Name: "sh", Args: []string{"-c", "echo out; echo err 1>&2"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(string(cmd.Stdout)) != "out" {
		t.Errorf("stdout = %q", cmd.Stdout)
	}
	if strings.TrimSpace(string(cmd.Stderr)) != "err" {
		t.Errorf("stderr = %q", cmd.Stderr)
	}
	if cmd.ExitCode != 0 {
		t.Errorf("exit = %d, want 0", cmd.ExitCode)
	}
}

func TestExecRunnerNonZeroExitIsNotError(t *testing.T) {
	cmd, err := ExecRunner{}.Run(context.Background(), Invocation{
		Name: "sh", Args: []string{"-c", "exit 3"},
	})
	if err != nil {
		t.Fatalf("non-zero exit should not be a Go error, got: %v", err)
	}
	if cmd.ExitCode != 3 {
		t.Errorf("exit = %d, want 3", cmd.ExitCode)
	}
}

func TestExecRunnerStartFailureIsError(t *testing.T) {
	cmd, err := ExecRunner{}.Run(context.Background(), Invocation{Name: "no-such-binary-xyzzy"})
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
	if cmd.ExitCode != -1 {
		t.Errorf("exit = %d, want -1", cmd.ExitCode)
	}
}

func TestExecRunnerEmptyName(t *testing.T) {
	if _, err := (ExecRunner{}).Run(context.Background(), Invocation{}); err == nil {
		t.Error("expected error for empty Name")
	}
}

func TestExecRunnerStdin(t *testing.T) {
	cmd, err := ExecRunner{}.Run(context.Background(), Invocation{
		Name: "cat", Stdin: []byte("piped input"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(cmd.Stdout) != "piped input" {
		t.Errorf("stdin not forwarded: %q", cmd.Stdout)
	}
}

func TestExecRunnerCaptureBound(t *testing.T) {
	// Emit ~5000 bytes but cap capture at 100.
	cmd, err := ExecRunner{MaxCaptureBytes: 100}.Run(context.Background(), Invocation{
		Name: "sh", Args: []string{"-c", "for i in $(seq 1 1000); do printf 'xxxxx'; done"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cmd.Stdout) != 100 {
		t.Errorf("captured %d bytes, want exactly 100 (bounded)", len(cmd.Stdout))
	}
}

func TestRunnerFuncAdapter(t *testing.T) {
	var got Invocation
	var r Runner = RunnerFunc(func(_ context.Context, inv Invocation) (Command, error) {
		got = inv
		return Command{Name: inv.Name, Stdout: []byte("canned")}, nil
	})
	cmd, _ := r.Run(context.Background(), Invocation{Name: "x", Args: []string{"y"}})
	if got.Name != "x" || string(cmd.Stdout) != "canned" {
		t.Errorf("adapter wrong: inv=%+v cmd=%+v", got, cmd)
	}
}

func TestSessionComposesRunnerAndRegistry(t *testing.T) {
	// A fake runner returns recorded git-status output; the real Default
	// registry compresses it. This proves the two halves compose without
	// either side knowing about the other.
	runner := RunnerFunc(func(_ context.Context, inv Invocation) (Command, error) {
		return Command{
			Name: inv.Name, Args: inv.Args,
			Stdout: []byte("On branch main\nYour branch is up to date.\n\tmodified: a.go\n"),
		}, nil
	})
	s := NewSession(runner, Default())
	cmd, res, err := s.Run(context.Background(), Invocation{Name: "git", Args: []string{"status"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Filter != "git-status" {
		t.Errorf("registry not applied: filter=%q", res.Filter)
	}
	if !strings.Contains(res.Text, "modified: a.go") || strings.Contains(res.Text, "On branch") {
		t.Errorf("compression wrong: %q", res.Text)
	}
	// Raw capture is still available alongside the compressed view.
	if !strings.Contains(string(cmd.Stdout), "On branch") {
		t.Errorf("raw capture should be preserved: %q", cmd.Stdout)
	}
}

func TestPackageRunShortcut(t *testing.T) {
	cmd, res, err := Run(context.Background(), Invocation{Name: "echo", Args: []string{"shortcut"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cmd.Stdout), "shortcut") || !strings.Contains(res.Text, "shortcut") {
		t.Errorf("Run shortcut wrong: cmd=%q res=%q", cmd.Stdout, res.Text)
	}
}

func TestPackageRunStreamShortcut(t *testing.T) {
	var lines []string
	_, res, err := RunStream(context.Background(),
		Invocation{Name: "sh", Args: []string{"-c", "echo one; echo two"}},
		func(ev StreamEvent) { lines = append(lines, ev.Line) },
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || !strings.Contains(res.Text, "one") {
		t.Errorf("RunStream shortcut wrong: lines=%v res=%q", lines, res.Text)
	}
}

func TestDefaultSessionEndToEnd(t *testing.T) {
	s := DefaultSession()
	_, res, err := s.Run(context.Background(), Invocation{Name: "echo", Args: []string{"hello"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "hello") {
		t.Errorf("passthrough lost output: %q", res.Text)
	}
	if res.Filter != "passthrough" {
		t.Errorf("echo should pass through: %q", res.Filter)
	}
}
