package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
)

func TestToolsEnforceSandboxAndWriteWiki(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "content/notes/one.md"), "note body\n")
	mustWrite(t, filepath.Join(root, "wiki/index.md"), "# Index\n")
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "wiki/outside")); err != nil {
		t.Fatal(err)
	}
	ws, err := newWorkspace(Config{Root: root, Limits: ToolLimits{ReadMaxBytes: 20, GlobMaxResults: 10, GrepMaxMatches: 10, GrepMaxFiles: 10, WriteMaxBytes: 100}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.read(context.Background(), readInput{Path: "../config.yaml"}); err == nil {
		t.Fatal("read allowed .. escape")
	}
	if _, err := ws.read(context.Background(), readInput{Path: "wiki/outside/file.md"}); err == nil {
		t.Fatal("read allowed symlink escape")
	}
	if _, err := ws.write(context.Background(), writeInput{Path: "content/notes/two.md", Content: "bad"}); err == nil {
		t.Fatal("write allowed notes")
	}
	out, err := ws.write(context.Background(), writeInput{Path: "wiki/a/b.md", Content: "ok"})
	if err != nil {
		t.Fatalf("write wiki subdir: %v", err)
	}
	if out.Path != "wiki/a/b.md" {
		t.Fatalf("write path = %q", out.Path)
	}
	data, err := os.ReadFile(filepath.Join(root, "wiki/a/b.md"))
	if err != nil || string(data) != "ok" {
		t.Fatalf("written data = %q, err = %v", string(data), err)
	}
}

func TestToolsTruncateOutputs(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "content/notes/one.md"), strings.Repeat("x", 50))
	mustWrite(t, filepath.Join(root, "wiki/a.md"), "alpha\nalpha\n")
	mustWrite(t, filepath.Join(root, "wiki/b.md"), "alpha\n")
	ws, err := newWorkspace(Config{Root: root, Limits: ToolLimits{ReadMaxBytes: 5, GlobMaxResults: 1, GrepMaxMatches: 1, GrepMaxFiles: 1, WriteMaxBytes: 100}})
	if err != nil {
		t.Fatal(err)
	}
	readOut, err := ws.read(context.Background(), readInput{Path: "content/notes/one.md"})
	if err != nil || !readOut.Truncated || len(readOut.Content) != 5 {
		t.Fatalf("read truncation = %+v, err = %v", readOut, err)
	}
	globOut, err := ws.glob(context.Background(), globInput{Path: "wiki", Pattern: "wiki/**/*.md"})
	if err != nil || !globOut.Truncated || len(globOut.Items) != 1 {
		t.Fatalf("glob truncation = %+v, err = %v", globOut, err)
	}
	grepOut, err := ws.grep(context.Background(), grepInput{Path: "wiki", Query: "alpha"})
	if err != nil || !grepOut.Truncated || len(grepOut.Matches) != 1 {
		t.Fatalf("grep truncation = %+v, err = %v", grepOut, err)
	}
}

func TestNewToolsReadOnlyOmitsWrite(t *testing.T) {
	root := t.TempDir()
	tools, err := NewTools(Config{Root: root, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	names := toolNames(t, tools)
	for _, want := range []string{"glob", "grep", "read"} {
		if !names[want] {
			t.Fatalf("read-only tools missing %q: %v", want, names)
		}
	}
	if names["write"] {
		t.Fatalf("read-only tools must not include write: %v", names)
	}
}

func TestNewToolsIncludesWriteByDefault(t *testing.T) {
	root := t.TempDir()
	tools, err := NewTools(Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	names := toolNames(t, tools)
	if !names["write"] {
		t.Fatalf("default tools must include write: %v", names)
	}
}

func toolNames(t *testing.T, tools []tool.BaseTool) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	for _, item := range tools {
		info, err := item.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		names[info.Name] = true
	}
	return names
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
