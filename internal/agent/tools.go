package agent

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

const (
	defaultGlobMaxResults = 200
	defaultGrepMaxFiles   = 50
	defaultGrepMaxMatches = 100
	defaultReadMaxBytes   = 256 << 10
	defaultWriteMaxBytes  = 512 << 10
)

type workspace struct {
	root      string
	wikiRoot  string
	notesRoot string
	limits    ToolLimits
	writeLock Locker
}

type globInput struct {
	Pattern string `json:"pattern" jsonschema:"required" jsonschema_description:"Glob pattern to match repo-relative paths, for example wiki/**/*.md or content/notes/**/*.md"`
	Path    string `json:"path" jsonschema:"required" jsonschema_description:"Repo-relative directory to search under. Allowed roots: wiki or content/notes"`
}

type globOutput struct {
	Items     []string `json:"items"`
	Truncated bool     `json:"truncated"`
}

type grepInput struct {
	Query string `json:"query" jsonschema:"required" jsonschema_description:"Literal text to search for"`
	Path  string `json:"path" jsonschema:"required" jsonschema_description:"Repo-relative file or directory under wiki or content/notes"`
}

type grepMatch struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

type grepOutput struct {
	Matches   []grepMatch `json:"matches"`
	Truncated bool        `json:"truncated"`
}

type readInput struct {
	Path string `json:"path" jsonschema:"required" jsonschema_description:"Repo-relative file under wiki or content/notes"`
}

type readOutput struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

type writeInput struct {
	Path    string `json:"path" jsonschema:"required" jsonschema_description:"Repo-relative file path under wiki"`
	Content string `json:"content" jsonschema:"required" jsonschema_description:"Complete file content to write"`
}

type writeOutput struct {
	Path      string `json:"path"`
	Bytes     int    `json:"bytes"`
	Changed   bool   `json:"changed"`
	Truncated bool   `json:"truncated"`
}

// NewTools creates the four Wiki Agent file tools.
func NewTools(cfg Config) ([]tool.BaseTool, error) {
	ws, err := newWorkspace(cfg)
	if err != nil {
		return nil, err
	}
	globTool, err := utils.InferTool("glob", "List files under wiki/** or content/notes/** using a glob pattern.", ws.glob)
	if err != nil {
		return nil, err
	}
	grepTool, err := utils.InferTool("grep", "Search literal text under wiki/** or content/notes/** and return limited snippets.", ws.grep)
	if err != nil {
		return nil, err
	}
	readTool, err := utils.InferTool("read", "Read one file under wiki/** or content/notes/** with a size limit.", ws.read)
	if err != nil {
		return nil, err
	}
	tools := []tool.BaseTool{globTool, grepTool, readTool}
	if !cfg.ReadOnly {
		writeTool, err := utils.InferTool("write", "Write one file under wiki/**. Parent directories are created automatically.", ws.write)
		if err != nil {
			return nil, err
		}
		tools = append(tools, writeTool)
	}
	return tools, nil
}

func newWorkspace(cfg Config) (*workspace, error) {
	root := filepath.Clean(cfg.Root)
	if root == "." || root == "" {
		return nil, errors.New("agent root is required")
	}
	limits := cfg.Limits
	if limits.GlobMaxResults <= 0 {
		limits.GlobMaxResults = defaultGlobMaxResults
	}
	if limits.GrepMaxFiles <= 0 {
		limits.GrepMaxFiles = defaultGrepMaxFiles
	}
	if limits.GrepMaxMatches <= 0 {
		limits.GrepMaxMatches = defaultGrepMaxMatches
	}
	if limits.ReadMaxBytes <= 0 {
		limits.ReadMaxBytes = defaultReadMaxBytes
	}
	if limits.WriteMaxBytes <= 0 {
		limits.WriteMaxBytes = defaultWriteMaxBytes
	}
	ws := &workspace{
		root:      root,
		wikiRoot:  filepath.Join(root, "wiki"),
		notesRoot: filepath.Join(root, "content", "notes"),
		limits:    limits,
		writeLock: cfg.WriteLock,
	}
	return ws, nil
}

func (w *workspace) glob(_ context.Context, input globInput) (globOutput, error) {
	base, _, err := w.resolveReadPath(input.Path)
	if err != nil {
		return globOutput{}, err
	}
	info, err := os.Stat(base)
	if err != nil {
		return globOutput{}, err
	}
	if !info.IsDir() {
		rel, err := w.repoRel(base)
		if err != nil {
			return globOutput{}, err
		}
		return globOutput{Items: []string{rel}}, nil
	}
	pattern := strings.TrimSpace(input.Pattern)
	if pattern == "" {
		pattern = "**/*"
	}
	matcher, err := globMatcher(pattern)
	if err != nil {
		return globOutput{}, err
	}
	out := globOutput{Items: []string{}}
	err = filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == base {
			return nil
		}
		if entry.IsDir() {
			if entry.Type()&os.ModeSymlink != 0 {
				return filepath.SkipDir
			}
			return nil
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil
		}
		if !w.isAllowedReadRealPath(resolved) {
			return nil
		}
		rel, err := w.repoRel(path)
		if err != nil || !matcher(rel) {
			return nil
		}
		out.Items = append(out.Items, rel)
		if len(out.Items) >= w.limits.GlobMaxResults {
			out.Truncated = true
			return errStopWalk
		}
		return nil
	})
	if errors.Is(err, errStopWalk) {
		err = nil
	}
	sort.Strings(out.Items)
	return out, err
}

func (w *workspace) grep(_ context.Context, input grepInput) (grepOutput, error) {
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return grepOutput{}, errors.New("query is required")
	}
	base, _, err := w.resolveReadPath(input.Path)
	if err != nil {
		return grepOutput{}, err
	}
	out := grepOutput{Matches: []grepMatch{}}
	filesSeen := map[string]bool{}
	visit := func(path string) error {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || !w.isAllowedReadRealPath(resolved) {
			return nil
		}
		info, err := os.Stat(resolved)
		if err != nil || info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return nil
		}
		if int64(len(data)) > w.limits.ReadMaxBytes {
			data = data[:w.limits.ReadMaxBytes]
		}
		lines := strings.Split(string(data), "\n")
		fileMatched := false
		for i, line := range lines {
			if !strings.Contains(strings.ToLower(line), strings.ToLower(query)) {
				continue
			}
			rel, err := w.repoRel(path)
			if err != nil {
				continue
			}
			fileMatched = true
			out.Matches = append(out.Matches, grepMatch{Path: rel, Line: i + 1, Snippet: trimSnippet(line)})
			if len(out.Matches) >= w.limits.GrepMaxMatches {
				out.Truncated = true
				return errStopWalk
			}
		}
		if fileMatched {
			filesSeen[path] = true
			if len(filesSeen) >= w.limits.GrepMaxFiles {
				out.Truncated = true
				return errStopWalk
			}
		}
		return nil
	}
	info, err := os.Stat(base)
	if err != nil {
		return grepOutput{}, err
	}
	if !info.IsDir() {
		if err := visit(base); err != nil && !errors.Is(err, errStopWalk) {
			return grepOutput{}, err
		}
		return out, nil
	}
	err = filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Type()&os.ModeSymlink != 0 {
				return filepath.SkipDir
			}
			return nil
		}
		return visit(path)
	})
	if errors.Is(err, errStopWalk) {
		err = nil
	}
	return out, err
}

func (w *workspace) read(_ context.Context, input readInput) (readOutput, error) {
	full, rel, err := w.resolveReadPath(input.Path)
	if err != nil {
		return readOutput{}, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return readOutput{}, err
	}
	if info.IsDir() {
		return readOutput{}, errors.New("read path must be a file")
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return readOutput{}, err
	}
	out := readOutput{Path: rel, Content: string(data)}
	if int64(len(data)) > w.limits.ReadMaxBytes {
		out.Content = string(data[:w.limits.ReadMaxBytes])
		out.Truncated = true
	}
	return out, nil
}

func (w *workspace) write(_ context.Context, input writeInput) (writeOutput, error) {
	if int64(len(input.Content)) > w.limits.WriteMaxBytes {
		return writeOutput{}, fmt.Errorf("content exceeds write limit of %d bytes", w.limits.WriteMaxBytes)
	}
	full, rel, err := w.resolveWritePath(input.Path)
	if err != nil {
		return writeOutput{}, err
	}
	if w.writeLock != nil {
		w.writeLock.Lock()
		defer w.writeLock.Unlock()
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return writeOutput{}, err
	}
	if err := w.ensureWriteParentInsideWiki(full); err != nil {
		return writeOutput{}, err
	}
	if existing, err := os.ReadFile(full); err == nil && string(existing) == input.Content {
		return writeOutput{Path: rel, Bytes: len(input.Content), Changed: false}, nil
	}
	if err := atomicWriteFile(full, []byte(input.Content), 0o644); err != nil {
		return writeOutput{}, err
	}
	return writeOutput{Path: rel, Bytes: len(input.Content), Changed: true}, nil
}

func (w *workspace) resolveReadPath(path string) (string, string, error) {
	rel, err := cleanRepoPath(path)
	if err != nil {
		return "", "", err
	}
	full := filepath.Join(w.root, filepath.FromSlash(rel))
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", "", err
	}
	if !w.isAllowedReadRealPath(resolved) {
		return "", "", fmt.Errorf("path %q is outside allowed read roots", rel)
	}
	return resolved, rel, nil
}

func (w *workspace) resolveWritePath(path string) (string, string, error) {
	rel, err := cleanRepoPath(path)
	if err != nil {
		return "", "", err
	}
	if rel != "wiki" && !strings.HasPrefix(rel, "wiki/") {
		return "", "", fmt.Errorf("write path %q is outside wiki", rel)
	}
	if rel == "wiki" || strings.HasSuffix(rel, "/") {
		return "", "", errors.New("write path must be a file under wiki")
	}
	return filepath.Join(w.root, filepath.FromSlash(rel)), rel, nil
}

func (w *workspace) ensureWriteParentInsideWiki(path string) error {
	if err := os.MkdirAll(w.wikiRoot, 0o755); err != nil {
		return err
	}
	realWiki, err := filepath.EvalSymlinks(w.wikiRoot)
	if err != nil {
		return err
	}
	realParent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return err
	}
	if !isSubpath(realParent, realWiki) {
		return fmt.Errorf("write path %q escapes wiki", path)
	}
	return nil
}

func (w *workspace) isAllowedReadRealPath(path string) bool {
	return isSubpath(path, w.realRoot(w.wikiRoot)) || isSubpath(path, w.realRoot(w.notesRoot))
}

func (w *workspace) realRoot(path string) string {
	real, err := filepath.EvalSymlinks(path)
	if err == nil {
		return real
	}
	return filepath.Clean(path)
}

func (w *workspace) repoRel(path string) (string, error) {
	rel, err := filepath.Rel(w.root, path)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func cleanRepoPath(path string) (string, error) {
	path = strings.TrimSpace(filepath.ToSlash(path))
	path = strings.TrimPrefix(path, "./")
	if path == "" || path == "." {
		return "", errors.New("path is required")
	}
	if strings.HasPrefix(path, "/") || filepath.IsAbs(path) {
		return "", errors.New("absolute paths are not allowed")
	}
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("path must not contain empty, . or .. segments")
		}
	}
	return path, nil
}

func isSubpath(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

var errStopWalk = errors.New("stop walk")

func globMatcher(pattern string) (func(string) bool, error) {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	if pattern == "" {
		pattern = "**/*"
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		switch ch {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				if i+2 < len(pattern) && pattern[i+2] == '/' {
					b.WriteString("(?:.*/)?")
					i += 2
				} else {
					b.WriteString(".*")
					i++
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, err
	}
	return func(path string) bool {
		path = filepath.ToSlash(path)
		return re.MatchString(path) || re.MatchString(filepath.Base(path))
	}, nil
}

func trimSnippet(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 240 {
		return s
	}
	return s[:240] + "..."
}

func atomicWriteFile(path string, data []byte, perm fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".aigoni-agent-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
