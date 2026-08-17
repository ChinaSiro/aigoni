package server

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (s *Server) wikiRoot() string {
	return filepath.Join(s.root, "wiki")
}

func (s *Server) readWikiDocs() ([]wikiDoc, error) {
	root := s.wikiRoot()
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	var docs []wikiDoc
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && strings.HasPrefix(filepath.ToSlash(path), filepath.ToSlash(filepath.Join(root, ".backups"))) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".md" || isWikiTmpFile(path) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		docs = append(docs, wikiDoc{Path: filepath.ToSlash(rel), Content: string(data)})
		return nil
	})
	sort.Slice(docs, func(i, j int) bool { return docs[i].Path < docs[j].Path })
	return docs, err
}

func (s *Server) writeWikiFile(path, data string) error {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if !allowedWikiWritePath(path) {
		return fmt.Errorf("invalid wiki path: %s", path)
	}
	full, err := s.fullWikiWritePath(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return atomicWriteWikiFile(full, []byte(strings.TrimSpace(data)+"\n"), 0o644)
}

// atomicWriteWikiFile 用「同目录临时文件 + fsync + rename」原子替换，
// 避免进程崩溃或并发写入时出现半截 Wiki 文件。临时文件使用隐藏的
// .aigoni-wiki-*.tmp 命名，Wiki 扫描按扩展名和前缀都会跳过它。
func atomicWriteWikiFile(full string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(full)
	tmp, err := os.CreateTemp(dir, ".aigoni-wiki-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	// 失败或 panic 时清理临时文件；成功 rename 后 tmpPath 已不存在。
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, full); err != nil {
		return err
	}
	tmpPath = ""
	return nil
}

func (s *Server) writeWikiFileIfChanged(path, data string) (bool, error) {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if !allowedWikiWritePath(path) {
		return false, fmt.Errorf("invalid wiki path: %s", path)
	}
	full, err := s.fullWikiWritePath(path)
	if err != nil {
		return false, err
	}
	current, err := os.ReadFile(full)
	if err == nil && normalizeWikiMarkdown(string(current)) == normalizeWikiMarkdown(data) {
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := s.writeWikiFile(path, data); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Server) fullWikiWritePath(path string) (string, error) {
	full := filepath.Join(s.root, filepath.FromSlash(path))
	root := s.wikiRoot()
	rel, err := filepath.Rel(root, full)
	if err != nil || strings.HasPrefix(rel, "..") || strings.HasPrefix(rel, string(filepath.Separator)+"..") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("invalid wiki path: %s", path)
	}
	return full, nil
}

// isWikiTmpFile 判断是否为原子写产生的隐藏临时文件（.aigoni-wiki-*.tmp）。
func isWikiTmpFile(path string) bool {
	return strings.HasPrefix(filepath.Base(path), ".aigoni-wiki-")
}

func normalizeWikiMarkdown(markdown string) string {
	markdown = strings.ReplaceAll(markdown, "\r\n", "\n")
	return strings.TrimSpace(markdown)
}

func (s *Server) backupWikiFile(path string) (string, error) {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if !allowedWikiWritePath(path) || path == "wiki/log.md" || strings.Contains(path, "..") || filepath.IsAbs(path) {
		return "", fmt.Errorf("invalid wiki backup path: %s", path)
	}
	full := filepath.Join(s.root, filepath.FromSlash(path))
	data, err := os.ReadFile(full)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	backupRel := filepath.ToSlash(filepath.Join("wiki", ".backups", time.Now().In(s.cfg.Timezone).Format("20060102-150405.000000000"), strings.TrimPrefix(path, "wiki/")))
	backupFull := filepath.Join(s.root, filepath.FromSlash(backupRel))
	if err := os.MkdirAll(filepath.Dir(backupFull), 0o755); err != nil {
		return "", err
	}
	if err := atomicWriteWikiFile(backupFull, data, 0o644); err != nil {
		return "", err
	}
	return backupRel, nil
}

func (s *Server) clearWikiBackups() (int, error) {
	s.wikiWriteMu.Lock()
	defer s.wikiWriteMu.Unlock()

	backupRoot := filepath.Join(s.wikiRoot(), ".backups")
	entries, err := os.ReadDir(backupRoot)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
			continue
		}
		path := filepath.Join(backupRoot, name)
		rel, err := filepath.Rel(backupRoot, path)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return removed, fmt.Errorf("invalid wiki backup directory: %s", name)
		}
		if err := os.RemoveAll(path); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

func allowedWikiWritePath(path string) bool {
	if path == "wiki/index.md" {
		return true
	}
	if strings.Contains(path, "..") || filepath.IsAbs(path) || filepath.Ext(path) != ".md" {
		return false
	}
	for _, prefix := range []string{"wiki/entities/", "wiki/concepts/", "wiki/sources/", "wiki/syntheses/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// syncWikiNoteRename updates live Wiki references without rewriting append-only history or backups.
func (s *Server) syncWikiNoteRename(oldPath, newPath string) error {
	oldPath = filepath.ToSlash(strings.TrimSpace(oldPath))
	newPath = filepath.ToSlash(strings.TrimSpace(newPath))
	if oldPath == "" || newPath == "" || oldPath == newPath {
		return nil
	}
	if !strings.HasPrefix(oldPath, "content/notes/") || !strings.HasPrefix(newPath, "content/notes/") {
		return nil
	}
	root := s.wikiRoot()
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return nil
	}

	s.wikiWriteMu.Lock()
	defer s.wikiWriteMu.Unlock()
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && strings.HasPrefix(filepath.ToSlash(path), filepath.ToSlash(filepath.Join(root, ".backups"))) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".md" || isWikiTmpFile(path) || filepath.ToSlash(path) == filepath.ToSlash(filepath.Join(root, "log.md")) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		updated := strings.ReplaceAll(string(data), oldPath, newPath)
		if updated == string(data) {
			return nil
		}
		return atomicWriteWikiFile(path, []byte(updated), 0o644)
	})
}
func (s *Server) appendWikiLogEntry(entry string) error {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return errors.New("empty wiki log entry")
	}
	path := filepath.Join(s.wikiRoot(), "log.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	prefix := ""
	if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) != "" {
		prefix = "\n\n"
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(prefix + entry + "\n")
	return err
}

func (s *Server) readPreviewPath(path string) ([]byte, error) {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if strings.Contains(path, "..") || filepath.IsAbs(path) {
		return nil, errors.New("invalid preview path")
	}
	if !strings.HasPrefix(path, "content/notes/") && !allowedWikiPreviewPath(path) {
		return nil, errors.New("preview path is not allowed")
	}
	full := filepath.Join(s.root, filepath.FromSlash(path))
	return os.ReadFile(full)
}

func allowedWikiPreviewPath(path string) bool {
	if path == "wiki/index.md" || path == "wiki/log.md" {
		return true
	}
	for _, prefix := range []string{"wiki/entities/", "wiki/concepts/", "wiki/sources/", "wiki/syntheses/"} {
		if strings.HasPrefix(path, prefix) && filepath.Ext(path) == ".md" {
			return true
		}
	}
	return false
}
