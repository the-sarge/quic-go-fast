//go:build !windows

package githooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type hookFixture struct {
	t               *testing.T
	repo, hook, bin string
}

func newHookFixture(t *testing.T) *hookFixture {
	t.Helper()
	hook, err := filepath.Abs("pre-commit")
	if err != nil {
		t.Fatal(err)
	}
	f := &hookFixture{t: t, repo: t.TempDir(), hook: hook, bin: t.TempDir()}
	f.git("init", "-q")
	f.git("config", "user.email", "fixture@example.invalid")
	f.git("config", "user.name", "Hook fixture")
	f.git("config", "core.hooksPath", "/dev/null")
	f.write("go.mod", "module fixture\n\ngo 1.26.0\n")
	f.write("integrationtests/gomodvendor/go.mod", "module vendorfixture\n\ngo 1.26.0\n")
	f.git("add", ".")
	f.git("commit", "-qm", "fixture")
	f.tool("go", "exit 0")
	f.tool("gofumpt", "exit 0")
	return f
}

func (f *hookFixture) git(args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = f.repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %q: %v\n%s", args, err, out)
	}
	return string(out)
}

func (f *hookFixture) write(name, content string) {
	f.t.Helper()
	path := filepath.Join(f.repo, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *hookFixture) tool(name, script string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.bin, name), []byte("#!/bin/bash\nset -eu\n"+script+"\n"), 0o755); err != nil {
		f.t.Fatal(err)
	}
}

func (f *hookFixture) run(wantSuccess bool, diagnostic string) {
	f.t.Helper()
	before := f.git("diff", "--binary", "HEAD")
	indexBefore := f.git("ls-files", "--stage", "-z")
	cmd := exec.Command("bash", f.hook)
	cmd.Dir = f.repo
	cmd.Env = append(os.Environ(), "PATH="+f.bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if (err == nil) != wantSuccess || !strings.Contains(string(out), diagnostic) {
		f.t.Fatalf("hook success = %v, want %v; diagnostic %q\n%s", err == nil, wantSuccess, diagnostic, out)
	}
	if after := f.git("diff", "--binary", "HEAD"); after != before {
		f.t.Fatal("hook changed caller files")
	}
	if after := f.git("ls-files", "--stage", "-z"); after != indexBefore {
		f.t.Fatal("hook changed index")
	}
}

func TestHookReportsFormatterFailureWithEmptyOutput(t *testing.T) {
	f := newHookFixture(t)
	f.write("example.go", "package fixture\n")
	f.git("add", "example.go")
	f.tool("gofumpt", "exit 23")
	f.run(false, "gofumpt failed")
}

func TestHookUsesStagedContentAndSafeFilenames(t *testing.T) {
	f := newHookFixture(t)
	for _, name := range []string{"space name.go", "-leading.go", "tab\tname.go", "line\nbreak.go", "[glob]*.go"} {
		f.write(name, "package staged\n")
		f.git("--literal-pathspecs", "add", "--", name)
		f.write(name, "package unstaged\n")
	}
	f.write("go.mod", "module unstaged\n")
	f.tool("gofumpt", `[[ "$1" == -d ]]
shift
[[ "$#" == 5 ]]
for file in "$@"; do
  [[ "$(cat "$file")" == 'package staged' ]]
done`)
	f.tool("go", `[[ "$*" == 'mod tidy -diff' ]]
[[ "$GOWORK" == off ]]
[[ "$(head -n 1 ../../go.mod)" == 'module fixture' ]]`)
	f.run(true, "")
}

func TestHookRejectsUnformattedStagedContent(t *testing.T) {
	f := newHookFixture(t)
	f.write("example.go", "package staged\n")
	f.git("add", "example.go")
	f.write("example.go", "package unstaged\n")
	f.tool("gofumpt", `[[ "$(cat "$2")" == 'package staged' ]]
echo 'formatting diff'`)
	f.run(false, "formatting diff")
}

func TestHookReportsModuleFailureWithoutChangingCaller(t *testing.T) {
	for _, output := range []string{"", "tidy diff"} {
		t.Run(output, func(t *testing.T) {
			f := newHookFixture(t)
			f.write("integrationtests/gomodvendor/go.mod", "module staged\n")
			f.git("add", ".")
			f.write("integrationtests/gomodvendor/go.mod", "module unstaged\n")
			f.tool("go", `[[ "$*" == 'mod tidy -diff' ]]
[[ "$(cat go.mod)" == 'module staged' ]]
echo 'tool side effect' > go.mod
printf '%s' '`+output+`'
exit 17`)
			f.run(false, "Module validation failed")
		})
	}
}

func TestHookSkipsFormatterWithoutStagedGoFiles(t *testing.T) {
	f := newHookFixture(t)
	f.write("removed.go", "package fixture\n")
	f.git("add", ".")
	f.git("commit", "-qm", "Go file")
	f.git("rm", "removed.go")
	f.write("unstaged.go", "not Go syntax")
	f.tool("gofumpt", "echo 'formatter must not run'\nexit 1")
	f.run(true, "")
}

func TestHookFormatsRenamedFiles(t *testing.T) {
	f := newHookFixture(t)
	f.write("old.go", "package fixture\n")
	f.git("add", ".")
	f.git("commit", "-qm", "Go file")
	f.git("mv", "old.go", "new name.go")
	f.tool("gofumpt", `[[ "$#" == 2 && "$2" == './new name.go' ]]
[[ "$(cat "$2")" == 'package fixture' ]]`)
	f.run(true, "")
}

func TestHookRejectsStagedGoSymlinks(t *testing.T) {
	for _, symlinks := range []string{"true", "false"} {
		t.Run(symlinks, func(t *testing.T) {
			f := newHookFixture(t)
			f.write("source", "package fixture\n")
			if err := os.Symlink("source", filepath.Join(f.repo, "link.go")); err != nil {
				t.Fatal(err)
			}
			f.git("add", ".")
			f.git("config", "core.symlinks", symlinks)
			if entry := f.git("ls-files", "--stage", "--", "link.go"); !strings.HasPrefix(entry, "120000 ") {
				t.Fatalf("expected staged symlink, got %q", entry)
			}
			f.tool("gofumpt", "echo 'formatter must not run'\nexit 99")
			f.run(false, "Staged Go input must be a regular file")
		})
	}
}
