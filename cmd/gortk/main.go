// Command gortk is an OPTIONAL developer/debug tool for the gortk package.
//
// It is NOT the integration path for codefly — codefly runs commands in-process
// and should call the gortk library directly (see INTEGRATION.md). This binary
// exists to iterate on filters from a terminal and to let non-Go callers pipe
// output through gortk.
//
// Usage:
//
//	# Compress the output of a command you run yourself (pipe it in):
//	go test ./... 2>&1 | gortk filter go test
//
//	# Run a command AND compress its output (mirrors rtk's proxy form):
//	gortk run -- go test ./...
//
//	# List the active filters:
//	gortk specs
//
// With --specs FILE you can layer extra JSON specs on top of the defaults to
// try new compression rules without recompiling.
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/codefly-dev/gortk"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gortk:", err)
		os.Exit(1)
	}
}

func run(argv []string) error {
	if len(argv) == 0 {
		return usage()
	}
	specsPath := ""
	stream := false
	// A tiny manual parse so flags can appear before the subcommand.
	var rest []string
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--specs":
			if i+1 >= len(argv) {
				return fmt.Errorf("--specs needs a file")
			}
			specsPath = argv[i+1]
			i++
		case "--stream":
			stream = true
		case "-h", "--help":
			return usage()
		default:
			rest = append(rest, argv[i])
		}
	}
	if len(rest) == 0 {
		return usage()
	}

	reg, err := registry(specsPath)
	if err != nil {
		return err
	}

	switch rest[0] {
	case "filter":
		return cmdFilter(reg, rest[1:])
	case "run":
		return cmdRun(reg, rest[1:], stream)
	case "specs":
		return cmdSpecs(specsPath)
	default:
		return fmt.Errorf("unknown subcommand %q (try: filter, run, specs)", rest[0])
	}
}

// cmdFilter reads stdin and compresses it as if it were the output of the
// command given by the remaining args.
func cmdFilter(reg *gortk.Registry, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("filter needs a command, e.g. `gortk filter go test`")
	}
	in, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	res := reg.Compress(gortk.CommandFromArgs(args, in, nil, 0))
	fmt.Print(res.Text)
	report(res)
	return nil
}

// cmdRun executes the command after `--`, then compresses its output. It uses
// the library's two halves explicitly: an ExecRunner (input) feeding the
// Registry (output), composed by a Session.
func cmdRun(reg *gortk.Registry, args []string, stream bool) error {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		return fmt.Errorf("run needs a command, e.g. `gortk run -- go test ./...`")
	}
	session := gortk.NewSession(gortk.ExecRunner{}, reg)
	inv := gortk.Invocation{Name: args[0], Args: args[1:]}

	var cmd gortk.Command
	var res gortk.Result
	var err error
	if stream {
		// Live lines go to stderr while the command runs; the compressed view
		// goes to stdout at the end. Demonstrates the streaming input half.
		cmd, res, err = session.RunStream(context.Background(), inv, func(ev gortk.StreamEvent) {
			fmt.Fprintf(os.Stderr, "%s| %s\n", ev.Stream, ev.Line)
		})
	} else {
		cmd, res, err = session.Run(context.Background(), inv)
	}
	if err != nil {
		return err
	}
	fmt.Print(res.Text)
	report(res)
	os.Exit(cmd.ExitCode)
	return nil
}

// cmdSpecs prints the active filter names.
func cmdSpecs(specsPath string) error {
	fmt.Println("built-in code filters:")
	fmt.Println("  go-test")
	fmt.Println("built-in specs:")
	for _, s := range gortk.DefaultSpecs() {
		fmt.Printf("  %s (match: %s)\n", s.Name, s.Match.Command)
	}
	if specsPath != "" {
		extra, err := loadSpecFile(specsPath)
		if err != nil {
			return err
		}
		fmt.Printf("extra specs from %s:\n", specsPath)
		for _, s := range extra {
			fmt.Printf("  %s (match: %s)\n", s.Name, s.Match.Command)
		}
	}
	return nil
}

func registry(specsPath string) (*gortk.Registry, error) {
	reg := gortk.Default()
	if specsPath == "" {
		return reg, nil
	}
	specs, err := loadSpecFile(specsPath)
	if err != nil {
		return nil, err
	}
	for _, s := range specs {
		if err := reg.RegisterSpec(s); err != nil {
			return nil, fmt.Errorf("spec %q: %w", s.Name, err)
		}
	}
	return reg, nil
}

func loadSpecFile(path string) ([]gortk.Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return gortk.LoadSpecs(data)
}

// report prints a one-line loss note to stderr so stdout stays clean for piping.
func report(res gortk.Result) {
	if !res.Lossless() {
		fmt.Fprintf(os.Stderr, "[gortk %s] %s\n", res.Filter, res.Truncation.Note)
	}
}

func usage() error {
	fmt.Fprint(os.Stderr, `gortk — compress command output for LLM context (dev/debug tool)

usage:
  gortk filter <cmd...> < output     compress piped output as if from <cmd>
  gortk run -- <cmd...>              run <cmd> and compress its output
  gortk run --stream -- <cmd...>     stream lines live, compress at the end
  gortk specs                        list active filters
  --specs FILE                       layer extra JSON specs on the defaults
`)
	return nil
}
