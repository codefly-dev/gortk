package gortk

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestRunStreamEmitsLinesInOrder(t *testing.T) {
	var mu sync.Mutex
	var stdout []string
	cmd, err := ExecRunner{}.RunStream(context.Background(),
		Invocation{Name: "sh", Args: []string{"-c", "echo a; echo b; echo c"}},
		func(ev StreamEvent) {
			if ev.Stream == StreamStdout {
				mu.Lock()
				stdout = append(stdout, ev.Line)
				mu.Unlock()
			}
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(stdout, ","); got != "a,b,c" {
		t.Errorf("streamed stdout order = %q, want a,b,c", got)
	}
	// The full capture is still returned for the batch parser.
	if strings.TrimSpace(string(cmd.Stdout)) != "a\nb\nc" {
		t.Errorf("final capture = %q", cmd.Stdout)
	}
	if cmd.ExitCode != 0 {
		t.Errorf("exit = %d", cmd.ExitCode)
	}
}

func TestRunStreamSeparatesStreams(t *testing.T) {
	var mu sync.Mutex
	seen := map[Stream][]string{}
	_, err := ExecRunner{}.RunStream(context.Background(),
		Invocation{Name: "sh", Args: []string{"-c", "echo out1; echo err1 1>&2; echo out2"}},
		func(ev StreamEvent) {
			mu.Lock()
			seen[ev.Stream] = append(seen[ev.Stream], ev.Line)
			mu.Unlock()
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(seen[StreamStdout], ",") != "out1,out2" {
		t.Errorf("stdout lines = %v", seen[StreamStdout])
	}
	if strings.Join(seen[StreamStderr], ",") != "err1" {
		t.Errorf("stderr lines = %v", seen[StreamStderr])
	}
}

func TestRunStreamNonZeroExit(t *testing.T) {
	cmd, err := ExecRunner{}.RunStream(context.Background(),
		Invocation{Name: "sh", Args: []string{"-c", "echo x; exit 7"}}, nil)
	if err != nil {
		t.Fatalf("non-zero exit should not error: %v", err)
	}
	if cmd.ExitCode != 7 {
		t.Errorf("exit = %d, want 7", cmd.ExitCode)
	}
	if strings.TrimSpace(string(cmd.Stdout)) != "x" {
		t.Errorf("capture lost: %q", cmd.Stdout)
	}
}

func TestRunStreamNilCallback(t *testing.T) {
	// nil onLine must still run and capture.
	cmd, err := ExecRunner{}.RunStream(context.Background(),
		Invocation{Name: "echo", Args: []string{"hi"}}, nil)
	if err != nil || strings.TrimSpace(string(cmd.Stdout)) != "hi" {
		t.Fatalf("nil callback path broken: err=%v out=%q", err, cmd.Stdout)
	}
}

func TestSessionRunStreamCompresses(t *testing.T) {
	var lines int
	s := DefaultSession()
	cmd, res, err := s.RunStream(context.Background(),
		Invocation{Name: "git", Args: []string{"status"}}, // not a real repo; just exercises the path
		func(StreamEvent) { lines++ },
	)
	_ = cmd
	_ = res
	// git status may fail outside a repo; we only assert the streaming path runs
	// without panicking and returns. Exit/err depend on environment.
	if err != nil && cmd.ExitCode == 0 {
		t.Errorf("inconsistent: err=%v exit=%d", err, cmd.ExitCode)
	}
}

func TestSessionRunStreamDegradesForNonStreamRunner(t *testing.T) {
	// A plain Runner (not a StreamRunner) must still work via Session.RunStream.
	runner := RunnerFunc(func(_ context.Context, inv Invocation) (Command, error) {
		return Command{Name: inv.Name, Args: inv.Args, Stdout: []byte("On branch main\n\tmodified: a.go\n")}, nil
	})
	called := false
	cmd, res, err := NewSession(runner, Default()).RunStream(context.Background(),
		Invocation{Name: "git", Args: []string{"status"}},
		func(StreamEvent) { called = true },
	)
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("a non-stream runner must not invoke the line callback")
	}
	if res.Filter != "git-status" || !strings.Contains(res.Text, "modified: a.go") {
		t.Errorf("degraded path did not compress: filter=%q text=%q", res.Filter, res.Text)
	}
	if !strings.Contains(string(cmd.Stdout), "On branch") {
		t.Errorf("raw capture missing: %q", cmd.Stdout)
	}
}
