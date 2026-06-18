package gortk

import (
	"reflect"
	"testing"
)

func TestParseCommandLine(t *testing.T) {
	cases := []struct {
		in   string
		name string
		args []string
	}{
		{"go test ./...", "go", []string{"test", "./..."}},
		{"git status", "git", []string{"status"}},
		{`git commit -m "a b c"`, "git", []string{"commit", "-m", "a b c"}},
		{`echo 'single quoted'`, "echo", []string{"single quoted"}},
		{"  go   test   ", "go", []string{"test"}},
		{"go test ./... | grep FAIL", "go", []string{"test", "./..."}},
		{"go test && echo done", "go", []string{"test"}},
		{"FOO=1 BAR=2 git push", "git", []string{"push"}},
		{`docker run -e K="v=1" img`, "docker", []string{"run", "-e", "K=v=1", "img"}},
		{"", "", nil},
		{"   ", "", nil},
		{`printf "line1\nline2"`, "printf", []string{`line1\nline2`}}, // \n stays literal in dquotes
		{`echo "a \"quoted\" word"`, "echo", []string{`a "quoted" word`}},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			name, args := ParseCommandLine(tc.in)
			if name != tc.name {
				t.Errorf("name = %q, want %q", name, tc.name)
			}
			if len(args) != 0 || len(tc.args) != 0 {
				if !reflect.DeepEqual(args, tc.args) {
					t.Errorf("args = %#v, want %#v", args, tc.args)
				}
			}
		})
	}
}

func TestCommandFromLineRoutesToFilter(t *testing.T) {
	cmd := CommandFromLine("git status --short", []byte("On branch main\n modified: a.go\n"), nil, 0)
	if cmd.Name != "git" || cmd.Sub() != "status" {
		t.Fatalf("parsed wrong: name=%q sub=%q", cmd.Name, cmd.Sub())
	}
	res := Default().Compress(cmd)
	if res.Filter != "git-status" {
		t.Errorf("shell-line command did not route to git-status: %q", res.Filter)
	}
}

func TestCommandFromArgs(t *testing.T) {
	cmd := CommandFromArgs([]string{"go", "test", "-json"}, []byte("out"), []byte("err"), 2)
	if cmd.Name != "go" || !reflect.DeepEqual(cmd.Args, []string{"test", "-json"}) {
		t.Fatalf("argv mapping wrong: %+v", cmd)
	}
	if cmd.ExitCode != 2 || string(cmd.Stdout) != "out" || string(cmd.Stderr) != "err" {
		t.Errorf("payload mapping wrong: %+v", cmd)
	}
}
