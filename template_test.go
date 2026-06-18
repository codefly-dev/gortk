package gortk

import (
	"strings"
	"testing"
)

func TestJSONGoTemplateNestedRange(t *testing.T) {
	// The rubocop shape: array of files, each with a nested offenses array. A Go
	// item_template ranges over the inner array — impossible with the simple
	// {a.b} syntax.
	f := compile(t, Spec{
		Name: "t", Match: MatchSpec{Command: "rubocop"},
		JSON: &JSONSpec{
			ArrayField:      "files",
			ItemTemplate:    "{{$p := .path}}{{range .offenses}}{{$p}}:{{.location.start_line}} [{{.cop_name}}] {{.message}}\n{{end}}",
			SummaryTemplate: "offenses in {count} file(s)",
		},
	})
	in := `{"files":[
		{"path":"a.rb","offenses":[
			{"cop_name":"Layout/Tabs","message":"tab found","location":{"start_line":3}},
			{"cop_name":"Lint/Void","message":"void expr","location":{"start_line":8}}
		]},
		{"path":"b.rb","offenses":[
			{"cop_name":"Style/Foo","message":"bad","location":{"start_line":1}}
		]}
	]}`
	res := f.Apply(Command{Stdout: []byte(in)})
	for _, want := range []string{
		"a.rb:3 [Layout/Tabs] tab found",
		"a.rb:8 [Lint/Void] void expr",
		"b.rb:1 [Style/Foo] bad",
		"offenses in 2 file(s)",
	} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("missing %q in:\n%s", want, res.Text)
		}
	}
}

func TestJSONGoTemplateConditional(t *testing.T) {
	f := compile(t, Spec{
		Name: "t", Match: MatchSpec{Command: "x"},
		JSON: &JSONSpec{
			ArrayField:   "xs",
			ItemTemplate: `{{.name}}{{if .err}} ERROR: {{.err}}{{end}}`,
		},
	})
	res := f.Apply(Command{Stdout: []byte(`{"xs":[{"name":"ok-task"},{"name":"bad-task","err":"boom"}]}`)})
	if !strings.Contains(res.Text, "ok-task\n") {
		t.Errorf("plain item wrong:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "bad-task ERROR: boom") {
		t.Errorf("conditional item wrong:\n%s", res.Text)
	}
}

func TestSimpleTemplateStillWorks(t *testing.T) {
	// Templates without {{ keep using the lightweight {a.b} resolver.
	f := compile(t, Spec{
		Name: "t", Match: MatchSpec{Command: "x"},
		JSON: &JSONSpec{ArrayField: "xs", ItemTemplate: "{a.b}-{c}"},
	})
	res := f.Apply(Command{Stdout: []byte(`{"xs":[{"a":{"b":"deep"},"c":7}]}`)})
	if !strings.Contains(res.Text, "deep-7") {
		t.Errorf("simple template broke: %q", res.Text)
	}
}

func TestBadGoTemplateRejectedAtCompile(t *testing.T) {
	s := Spec{
		Name: "t", Match: MatchSpec{Command: "x"},
		JSON: &JSONSpec{ArrayField: "xs", ItemTemplate: "{{.unclosed"},
	}
	if err := s.Validate(); err == nil {
		t.Error("expected compile error for malformed Go template")
	}
}
