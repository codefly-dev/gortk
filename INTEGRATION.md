# Integrating gortk with codefly

## TL;DR

- **Use it as a library, not a CLI.** codefly already runs every command
  in-process in Go and holds the raw `stdout`/`stderr`/`exitCode`. Compressing is
  one function call on that result. rtk ships a CLI only because it's an external
  proxy for third-party tools (Claude Code, Cursor) that shell out — codefly is
  not in that position.
- The CLI in `cmd/gortk` is an **optional dev/debug tool** (iterate on filters,
  let non-Go callers pipe through it). It is not on the runtime path.
- Drop gortk in at the two places that already capture command output:
  `core/code/shell_exec.go` and `cli/pkg/gateway/server.go` (`RunCommand`).

## Do we need a CLI?

No — not for the runtime. Here's the decision matrix:

| Need | Answer |
|---|---|
| Compress output inside an agent / the gateway | **Library.** You already have the bytes; call `Compress`. |
| Iterate on a new filter spec from a terminal | CLI (`gortk filter …`) — convenient, optional. |
| A non-Go process wants compression | CLI (`gortk run -- …`) — optional. |
| Match rtk's transparent shell-hook UX | Not applicable; codefly isn't a shell proxy. |

So: build against the library; keep the CLI around as a dev tool.

## Where to call it

Both call sites produce exactly what `gortk.Command` needs. Pick the seam that
matches who assembles the model-facing text.

### Option A — gateway `RunCommand` (smallest change)

`cli/pkg/gateway/server.go:879` already returns `{Stdout, Stderr, ExitCode}`.
Compress in place right before returning:

```go
res := gortk.Default().Compress(gortk.CommandFromArgs(
    append([]string{req.Command}, req.Args...),
    stdout.Bytes(), stderr.Bytes(), exitCode,
))
return &gatewayv1.RunCommandResponse{
    ExitCode: int32(exitCode),
    Stdout:   res.Text, // compressed view for the model
    Stderr:   stderr.String(),
}, nil
```

This is the one-liner win. It's safe because gortk is lossless for any command
without a dedicated filter, and `res.Truncation` records anything that was
dropped.

### Option B — keep raw, add a compressed field (recommended)

`core/code/shell_exec.go` is the **sanctioned raw-exec boundary** (security
reviewed). Don't mutate its output there. Instead compress at the point where
Mind turns a tool result into context, so the raw bytes stay available to
anything else that wants them.

Minimal shape (a proto field + a helper); keep `Stdout`/`Stderr` raw and add:

```go
// in the response assembly that feeds Mind:
view := gortk.Default().Compress(gortk.CommandFromArgs(
    argv, []byte(resp.Stdout), []byte(resp.Stderr), int(resp.ExitCode),
))
resp.LlmOutput = view.Text          // new field: what the model sees
resp.OutputTruncated = view.Truncation.Happened
resp.OutputTruncationNote = view.Truncation.Note
```

Mind uses `LlmOutput` for context and can fall back to raw `Stdout` when an
agent needs the full thing (e.g. parsing a diff). This preserves gortk's
"lossless-by-default, honest-about-loss" contract end to end.

### Shell-line commands (`sh -c "…"`)

When the command was run as a shell line rather than argv (codefly's
`ShellExecRequest.Command`), use the line adapter — it tokenizes enough to route
to a filter:

```go
cmd := gortk.CommandFromLine(req.Command, []byte(resp.Stdout), []byte(resp.Stderr), int(resp.ExitCode))
view := gortk.Default().Compress(cmd)
```

## The input/output split (and where codefly sits)

gortk separates **running** (`Runner` / `Invocation` → `Command`) from
**parsing** (`Registry.Compress(Command)` → `Result`). codefly already owns a
hardened executor (`shellExec`: process groups, timeouts, sandbox, traversal
checks), so the recommended posture is:

- **Keep codefly's executor as the input half.** Do not adopt `ExecRunner` on
  the runtime path — it's the standalone/CLI runner and lacks your security
  hardening.
- **Use gortk's output half** (`Registry.Compress`) on the captured bytes.

If you want a uniform seam, wrap `shellExec` as a `gortk.Runner` so callers can
use a `Session` while execution still goes through your sanctioned path:

```go
// codefly side — adapt shellExec to gortk.Runner without changing it.
func (s *DefaultCodeServer) gortkRunner() gortk.Runner {
    return gortk.RunnerFunc(func(ctx context.Context, inv gortk.Invocation) (gortk.Command, error) {
        resp, err := s.shellExec(ctx, &codev0.ShellExecRequest{
            Args:           append([]string{inv.Name}, inv.Args...),
            WorkDir:        inv.Dir,
            Env:            inv.Env,
            TimeoutSeconds: int32(inv.Timeout.Seconds()),
        })
        if err != nil {
            return gortk.Command{}, err
        }
        se := resp.GetShellExec()
        return gortk.CommandFromArgs(
            append([]string{inv.Name}, inv.Args...),
            []byte(se.Stdout), []byte(se.Stderr), int(se.ExitCode),
        ), nil
    })
}

// Then compose with the output half:
session := gortk.NewSession(s.gortkRunner(), gortk.Default())
cmd, view, err := session.Run(ctx, gortk.Invocation{Name: "go", Args: []string{"test", "./..."}})
```

This gives Mind one call site (`session.Run`) while keeping execution inside the
security-reviewed `shellExec`, and leaves the raw `cmd` available next to the
compressed `view`.

## One registry, reused

`gortk.Default()` allocates and compiles filters; build it once and share it
(it's read-only after construction, safe for concurrent `Compress`).

```go
var compressor = gortk.Default() // package-level, built once

func compress(argv []string, out, err []byte, code int) gortk.Result {
    return compressor.Compress(gortk.CommandFromArgs(argv, out, err, code))
}
```

To add codefly-specific rules without touching this repo, ship a JSON spec file
and layer it on at startup:

```go
specs, _ := gortk.LoadSpecs(os.ReadFile("codefly-filters.json"))
for _, s := range specs { _ = compressor.RegisterSpec(s) }
```

## Relationship to the existing structured runners

codefly's `core/runners/*` (pytest JUnit, `go test -json`, cargo, jest) already
produce structured, compact results — **keep using them** for those commands.
gortk is for the generic command tail that currently falls through to the raw
2 MiB `boundedBuffer` in `shell_exec.go`: arbitrary `git`, `docker`, `make`,
lint, and one-off shell commands. That's the gap it fills.

This mirrors rtk's own architecture: ~60 hand-written parsers in `src/cmds/**`
for rich formats, plus a declarative engine for everything else.
```
