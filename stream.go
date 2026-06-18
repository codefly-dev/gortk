package gortk

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
)

// Streaming input. RunStream is for long-running commands where you want output
// live (progress to the user/agent, early signals) instead of only at the end.
// It stays on the INPUT side of the split: it emits raw lines as they arrive and
// returns the same full Command that batch Run does, so the OUTPUT half
// (Registry.Compress) is unchanged. Compression stays whole-output — most
// filters (match_output, json, head/tail) need the complete result — so the
// model-facing compression still happens once at the end, on the returned
// Command.

// maxScanTokenBytes bounds a single line for the streaming scanner so a very
// long line can't blow out memory; longer lines are still captured raw in the
// returned Command (subject to the byte cap), just not delivered as one event.
const maxScanTokenBytes = 4 << 20

// Stream identifies which standard stream a StreamEvent came from.
type Stream int

const (
	StreamStdout Stream = iota
	StreamStderr
)

func (s Stream) String() string {
	if s == StreamStderr {
		return "stderr"
	}
	return "stdout"
}

// StreamEvent is one line of output observed while a command runs.
type StreamEvent struct {
	Stream Stream
	Line   string // the line, without its trailing newline
}

// StreamFunc receives output lines as they are produced. It is called serially
// (stdout and stderr never overlap), so implementations need no locking.
type StreamFunc func(StreamEvent)

// StreamRunner is an optional capability: a Runner that can also stream output
// line-by-line. ExecRunner implements it. Hosts whose executor can't stream
// implement only Runner; Session.RunStream degrades gracefully for those.
type StreamRunner interface {
	Runner
	RunStream(ctx context.Context, inv Invocation, onLine StreamFunc) (Command, error)
}

var _ StreamRunner = ExecRunner{}

// RunStream runs inv, invoking onLine for each line of stdout/stderr as it is
// produced, and returns the full captured Command when the process exits. Exit
// semantics match Run: a non-zero exit is reported in Command.ExitCode, not as
// an error; a Go error means the process could not run to completion.
func (r ExecRunner) RunStream(ctx context.Context, inv Invocation, onLine StreamFunc) (Command, error) {
	if inv.Name == "" {
		return Command{}, errors.New("gortk: invocation has empty Name")
	}
	if inv.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, inv.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, inv.Name, inv.Args...)
	cmd.Dir = inv.Dir
	if len(inv.Env) > 0 {
		cmd.Env = append(os.Environ(), inv.Env...)
	}
	if len(inv.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(inv.Stdin)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return Command{ExitCode: -1}, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return Command{ExitCode: -1}, err
	}

	limit := r.MaxCaptureBytes
	if limit == 0 {
		limit = DefaultMaxCaptureBytes
	}
	var outBuf, errBuf boundedBuffer
	outBuf.limit, errBuf.limit = limit, limit

	if err := cmd.Start(); err != nil {
		return Command{Name: inv.Name, Args: inv.Args, ExitCode: -1}, err
	}

	// Serialize callbacks so the caller sees one line at a time across both
	// streams. Each pipe is tee'd into its bounded buffer so the returned
	// Command holds the exact raw bytes (newlines included), independent of how
	// the scanner chunks lines.
	var mu sync.Mutex
	var wg sync.WaitGroup
	pump := func(pipe io.Reader, buf *boundedBuffer, stream Stream) {
		defer wg.Done()
		sc := bufio.NewScanner(io.TeeReader(pipe, buf))
		sc.Buffer(make([]byte, 0, 64*1024), maxScanTokenBytes)
		for sc.Scan() {
			if onLine != nil {
				mu.Lock()
				onLine(StreamEvent{Stream: stream, Line: sc.Text()})
				mu.Unlock()
			}
		}
	}
	wg.Add(2)
	go pump(stdoutPipe, &outBuf, StreamStdout)
	go pump(stderrPipe, &errBuf, StreamStderr)
	wg.Wait() // pipes drained to EOF before Wait, per os/exec contract

	runErr := cmd.Wait()
	captured := Command{
		Name:   inv.Name,
		Args:   inv.Args,
		Stdout: outBuf.Bytes(),
		Stderr: errBuf.Bytes(),
	}
	if runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			captured.ExitCode = ee.ExitCode()
			return captured, nil
		}
		captured.ExitCode = -1
		return captured, runErr
	}
	return captured, nil
}

// RunStream is the streaming convenience shortcut, mirroring the package-level
// Run: it streams lines from the default in-process session and compresses the
// final output. Reuses the shared default session.
func RunStream(ctx context.Context, inv Invocation, onLine StreamFunc) (Command, Result, error) {
	return sharedDefaultSession().RunStream(ctx, inv, onLine)
}

// RunStream executes the invocation with live line callbacks, then compresses
// the final output. If the session's Runner does not support streaming, it
// degrades to a batch Run (onLine is not called) so callers can use one code
// path regardless of runner capability.
func (s *Session) RunStream(ctx context.Context, inv Invocation, onLine StreamFunc) (Command, Result, error) {
	sr, ok := s.Runner.(StreamRunner)
	if !ok {
		return s.Run(ctx, inv)
	}
	cmd, err := sr.RunStream(ctx, inv, onLine)
	if err != nil {
		return cmd, Result{}, err
	}
	return cmd, s.Registry.Compress(cmd), nil
}
