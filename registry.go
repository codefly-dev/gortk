package gortk

import "strings"

// DefaultMaxBytes bounds the passthrough (and post-filter) output size. It is
// the last line of defence against a runaway command flooding the context. The
// value mirrors codefly's per-test-case cap (32 KiB) rather than its 2 MiB raw
// shell cap, because gortk output is meant to already be compact.
const DefaultMaxBytes = 32 * 1024

// Registry holds an ordered set of filters and applies the first one that
// matches. The zero value is not usable; build one with New.
type Registry struct {
	filters  []Filter
	maxBytes int
}

// New builds a Registry with the given filters (first match wins) and the
// default size bound.
func New(filters ...Filter) *Registry {
	return &Registry{filters: filters, maxBytes: DefaultMaxBytes}
}

// Default returns a Registry wired with the built-in filters: the hand-written
// structured parsers (go test) plus the embedded declarative Specs
// (golangci-lint, git status). This is the intended entry point for embedding
// gortk in an agent: keep one Default registry and call Compress on every
// command's output.
//
// It panics only if the embedded defaults are malformed, which is a build-time
// guarantee covered by tests.
func Default() *Registry {
	r := New(GoTest{})
	for _, s := range DefaultSpecs() {
		f, err := s.Compile()
		if err != nil {
			// Be resilient: one malformed embedded/community spec must not break
			// the whole registry. TestEmbeddedDefaultsAllCompile fails loudly if
			// any built-in spec is invalid, so this only ever skips at runtime
			// for third-party specs added later.
			continue
		}
		r.Register(f)
	}
	return r
}

// RegisterSpec compiles a Spec and appends it as a lowest-priority filter.
func (r *Registry) RegisterSpec(s Spec) error {
	f, err := s.Compile()
	if err != nil {
		return err
	}
	r.Register(f)
	return nil
}

// WithMaxBytes returns a copy of the registry with a different size bound.
func (r *Registry) WithMaxBytes(n int) *Registry {
	cp := *r
	cp.maxBytes = n
	return &cp
}

// Register appends a filter, giving it lowest priority. Use this to add
// project-specific filters on top of Default().
func (r *Registry) Register(f Filter) *Registry {
	r.filters = append(r.filters, f)
	return r
}

// Compress runs the first matching filter, then bounds the result size. If no
// filter matches, it returns a lossless passthrough of stdout+stderr (still
// size-bounded). Compress never errors and never panics on a well-formed
// Command — a command with no special handling is simply passed through.
func (r *Registry) Compress(cmd Command) Result {
	for _, f := range r.filters {
		if f.Match(cmd.Name, cmd.Args) {
			return r.bound(f.Apply(cmd))
		}
	}
	return r.bound(passthrough(cmd))
}

// passthrough joins stderr and stdout verbatim. It is lossless by construction;
// only r.bound may later trim it for size.
func passthrough(cmd Command) Result {
	var b strings.Builder
	if len(cmd.Stderr) > 0 {
		b.Write(cmd.Stderr)
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte('\n')
		}
	}
	b.Write(cmd.Stdout)
	return Result{Text: b.String(), Filter: "passthrough"}
}

// bound trims a Result's Text to maxBytes, keeping the tail (where failures and
// summaries usually live) and recording the loss.
func (r *Registry) bound(res Result) Result {
	if r.maxBytes <= 0 || len(res.Text) <= r.maxBytes {
		return res
	}
	dropped := len(res.Text) - r.maxBytes
	res.Text = "… (" + itoa(dropped) + " bytes trimmed) …\n" + res.Text[dropped:]
	res.Truncation.dropBytes(dropped, "output exceeded size bound; kept tail")
	return res
}
