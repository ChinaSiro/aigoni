package content

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Repository struct {
	PostsDir string
	PagesDir string
	NotesDir string
}

// createMu serializes ID allocation and file creation within this process.
var createMu sync.Mutex

var (
	safeSlug         = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
	noteTitleIllegal = regexp.MustCompile(`[\x00-\x1f\x7f\\/:*?"<>|]`)
	noteTitleSpace   = regexp.MustCompile(`\s+`)
)

const noteTitleMax = 50

func NewRepository(postsDir, pagesDir, notesDir string) *Repository {
	return &Repository{PostsDir: postsDir, PagesDir: pagesDir, NotesDir: notesDir}
}

func (r *Repository) List(typ Type) ([]*Item, error) {
	root, err := r.root(typ)
	if err != nil {
		return nil, err
	}
	items := []*Item{}
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return items, nil
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		item, err := r.GetByPath(path, typ)
		if err != nil {
			return err
		}
		items = append(items, item)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sortItems(items, typ)
	return items, nil
}

func (r *Repository) GetByID(id string, typ Type) (*Item, error) {
	if typ == TypeNote {
		resolved, err := r.resolveNoteID(id)
		if err != nil {
			return nil, err
		}
		id = resolved
	}
	path, err := r.pathForID(id, typ)
	if err != nil {
		return nil, err
	}
	return r.GetByPath(path, typ)
}

// StableID returns the public stable ID for a content item. Historical note
// filenames can contain a title and random suffix, but their date/sequence
// prefix remains the unique editor ID.
func StableID(id string, typ Type) string {
	if typ != TypeNote {
		return id
	}
	dir, base := filepath.Split(filepath.ToSlash(id))
	base = strings.TrimSuffix(base, ".md")
	parts := strings.Split(base, "-")
	if len(parts) < 4 || len(parts[0]) != 4 || len(parts[1]) != 2 || len(parts[2]) != 2 {
		return id
	}
	if _, err := strconv.Atoi(parts[0] + parts[1] + parts[2] + parts[3]); err != nil {
		return id
	}
	return filepath.ToSlash(filepath.Join(dir, strings.Join(parts[:4], "-")))
}

// resolveNoteID accepts both the stored note filename ID and its stable
// year/date/sequence prefix. Older notes may still have title and random
// suffixes in their filenames, while the prefix remains unique and stable.
func (r *Repository) resolveNoteID(id string) (string, error) {
	if strings.Contains(id, "..") || filepath.IsAbs(id) || id == "" {
		return "", errors.New("invalid content id")
	}
	root, err := r.root(TypeNote)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(filepath.FromSlash(id))
	if clean == "." || clean == string(filepath.Separator)+"" {
		return "", errors.New("invalid content id")
	}
	exact := filepath.Join(root, clean)
	if filepath.Ext(exact) != ".md" {
		exact += ".md"
	}
	if _, err := os.Stat(exact); err == nil {
		return filepath.ToSlash(strings.TrimSuffix(filepath.ToSlash(clean), ".md")), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	dir, base := filepath.Split(clean)
	base = strings.TrimSuffix(base, ".md")
	entries, err := os.ReadDir(filepath.Join(root, dir))
	if errors.Is(err, os.ErrNotExist) {
		return "", os.ErrNotExist
	}
	if err != nil {
		return "", err
	}
	var matches []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		stem := strings.TrimSuffix(entry.Name(), ".md")
		if strings.HasPrefix(stem, base+"-") {
			matches = append(matches, entry.Name())
		}
	}
	if len(matches) == 0 {
		return "", os.ErrNotExist
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous note id %q", id)
	}
	return filepath.ToSlash(filepath.Join(dir, strings.TrimSuffix(matches[0], ".md"))), nil
}

func (r *Repository) GetBySlug(slug string, typ Type) (*Item, error) {
	items, err := r.List(typ)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.Slug == slug {
			return item, nil
		}
	}
	return nil, os.ErrNotExist
}

func (r *Repository) GetByPath(path string, typ Type) (*Item, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	item, err := Parse(data, typ)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	root, err := r.root(typ)
	if err != nil {
		return nil, err
	}
	id, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	item.ID = filepath.ToSlash(strings.TrimSuffix(id, ".md"))
	item.Path = path
	return item, nil
}

func (r *Repository) Save(input SaveInput) (*Item, error) {
	if input.Type == TypeNote {
		return r.saveNote(input)
	}
	if input.ID == "" {
		createMu.Lock()
		defer createMu.Unlock()

		id, err := r.newDatedID(input.Type, input.Date)
		if err != nil {
			return nil, err
		}
		input.ID = id
		if input.Slug == "" {
			input.Slug = filepath.Base(id)
		}
	}
	path, err := r.pathForID(input.ID, input.Type)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	data, err := FrontMatter(input)
	if err != nil {
		return nil, err
	}
	if err := atomicWriteFile(path, data, 0o644); err != nil {
		return nil, err
	}
	return r.GetByPath(path, input.Type)
}

// saveNote keeps the date/sequence prefix stable while the title suffix follows the title.
func (r *Repository) saveNote(input SaveInput) (*Item, error) {
	if input.ID == "" {
		return r.createNote(input)
	}
	resolved, err := r.resolveNoteID(input.ID)
	if err == nil {
		input.ID = resolved
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if newID, ok := noteIDForTitle(input.ID, input.Title); ok && newID != input.ID {
		if err := r.renameNoteFiles(input.ID, newID, &input); err != nil {
			return nil, err
		}
		input.ID = newID
	}
	return r.writeNote(input)
}

// createNote 串行分配数字 ID 并立即落盘，避免并发创建得到相同 ID。
func (r *Repository) createNote(input SaveInput) (*Item, error) {
	createMu.Lock()
	defer createMu.Unlock()

	id, err := r.newNoteID(input)
	if err != nil {
		return nil, err
	}
	input.ID = id
	return r.writeNote(input)
}

func (r *Repository) writeNote(input SaveInput) (*Item, error) {
	path, err := r.pathForID(input.ID, TypeNote)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	data, err := NoteFrontMatter(input)
	if err != nil {
		return nil, err
	}
	if err := atomicWriteFile(path, data, 0o644); err != nil {
		return nil, err
	}
	return r.GetByPath(path, TypeNote)
}

// noteIDForTitle keeps the year/date/sequence prefix and replaces only the title suffix.
func noteIDForTitle(oldID, title string) (string, bool) {
	year, dateSeq, ok := splitNoteID(oldID)
	if !ok {
		return "", false
	}
	name := dateSeq
	if titlePart := cleanNoteTitle(title); titlePart != "" {
		name += "-" + titlePart
	}
	return filepath.ToSlash(filepath.Join(year, name)), true
}

// splitNoteID parses the year directory and YYYY-MM-DD-sequence note prefix.
func splitNoteID(id string) (year, dateSeq string, ok bool) {
	dir, name := filepath.Split(filepath.FromSlash(strings.TrimSpace(id)))
	year = strings.TrimSuffix(filepath.ToSlash(dir), "/")
	name = strings.TrimSuffix(name, ".md")
	parts := strings.Split(name, "-")
	if year == "" || len(parts) < 4 || len(parts[0]) != 4 || len(parts[1]) != 2 || len(parts[2]) != 2 {
		return "", "", false
	}
	if year != parts[0] {
		return "", "", false
	}
	if _, err := time.Parse("2006-01-02", strings.Join(parts[:3], "-")); err != nil {
		return "", "", false
	}
	if _, err := strconv.Atoi(parts[3]); err != nil {
		return "", "", false
	}
	return year, strings.Join(parts[:4], "-"), true
}

// renameNoteFiles renames a note and its same-name assets directory, then rewrites
// body asset URLs so changing a title does not break embedded resources.
func (r *Repository) renameNoteFiles(oldID, newID string, input *SaveInput) error {
	oldPath, err := r.pathForID(oldID, TypeNote)
	if err != nil {
		return err
	}
	newPath, err := r.pathForID(newID, TypeNote)
	if err != nil {
		return err
	}
	if oldPath == newPath {
		return nil
	}
	if _, err := os.Stat(oldPath); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("note path already exists: %s", newID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return err
	}

	oldAssets := strings.TrimSuffix(oldPath, ".md") + ".assets"
	newAssets := strings.TrimSuffix(newPath, ".md") + ".assets"
	assetsExist := false
	if _, err := os.Stat(oldAssets); err == nil {
		assetsExist = true
		if _, err := os.Stat(newAssets); err == nil {
			return fmt.Errorf("note assets path already exists: %s", newID)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.Rename(oldPath, newPath); err != nil {
		return err
	}
	if assetsExist {
		if err := os.Rename(oldAssets, newAssets); err != nil {
			_ = os.Rename(newPath, oldPath)
			return err
		}
		oldPrefix := noteAssetsURLPrefix(r.NotesDir, oldAssets)
		newPrefix := noteAssetsURLPrefix(r.NotesDir, newAssets)
		if oldPrefix != "" && newPrefix != "" {
			input.Body = strings.ReplaceAll(input.Body, oldPrefix, newPrefix)
		}
	}
	return nil
}

// noteAssetsURLPrefix derives the public asset prefix for a note assets directory.
func noteAssetsURLPrefix(notesDir, assetDir string) string {
	rel, err := filepath.Rel(notesDir, assetDir)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return ""
	}
	return "/assets/" + filepath.Base(notesDir) + "/" + filepath.ToSlash(rel)
}

// cleanNoteTitle sanitizes a title into a readable, filesystem-safe filename part.
func cleanNoteTitle(value string) string {
	value = noteTitleIllegal.ReplaceAllString(value, "")
	value = noteTitleSpace.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-.")
	if runes := []rune(value); len(runes) > noteTitleMax {
		value = strings.TrimRight(string(runes[:noteTitleMax]), "-.")
	}
	return value
}

// atomicWriteFile writes a complete replacement beside the target, syncs it, then renames it.
// Readers therefore see either the old file or the new file, never a partially written Markdown file.
func atomicWriteFile(path string, data []byte, perm fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".aigoni-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

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
	return os.Rename(tmpPath, path)
}

// newNoteID generates a note ID as year/YYYY-MM-DD-sequence-title.
// The date/sequence prefix is allocated independently from the title suffix.
func (r *Repository) newNoteID(input SaveInput) (string, error) {
	date := input.Date
	if date.IsZero() {
		date = time.Now().UTC()
	}
	root, err := r.root(TypeNote)
	if err != nil {
		return "", err
	}
	year := date.Format("2006")
	prefix := date.Format("2006-01-02")

	matches, err := filepath.Glob(filepath.Join(root, year, prefix+"-*.md"))
	if err != nil {
		return "", err
	}
	maxID := 0
	for _, match := range matches {
		if id := seqFromID(match); id > maxID {
			maxID = id
		}
	}

	name := fmt.Sprintf("%s-%d", prefix, maxID+1)
	if titlePart := cleanNoteTitle(input.Title); titlePart != "" {
		name += "-" + titlePart
	}
	return filepath.ToSlash(filepath.Join(year, name)), nil
}

func (r *Repository) NewPostDraft(now time.Time) (*Item, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	createMu.Lock()
	defer createMu.Unlock()

	id, err := r.newID(SaveInput{Type: TypePost, Date: now})
	if err != nil {
		return nil, err
	}
	title := filepath.Base(id)
	return r.Save(SaveInput{
		ID:      id,
		Type:    TypePost,
		Title:   title,
		Date:    now,
		Publish: false,
		Slug:    title,
	})
}

// NewPageDraft 新建一条未公开的固定文稿草稿，文件名使用创建日期和当天数字 ID。
func (r *Repository) NewPageDraft(now time.Time) (*Item, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	createMu.Lock()
	defer createMu.Unlock()

	id, err := r.newDatedID(TypePage, now)
	if err != nil {
		return nil, err
	}
	return r.Save(SaveInput{
		ID:      id,
		Type:    TypePage,
		Title:   "未命名页面",
		Date:    now,
		Slug:    id,
		Publish: false,
	})
}

// NewNoteDraft 新建一条空白私人笔记：默认标题"未命名笔记"，文件名带标题段，
// 保证新建即落盘、立即进入编辑态。标题后续可改，保存时自动同步文件名。
func (r *Repository) NewNoteDraft(now time.Time) (*Item, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return r.saveNote(SaveInput{Type: TypeNote, Title: "未命名笔记", Date: now})
}

func (r *Repository) Delete(id string, typ Type) error {
	var path string
	var err error
	if typ == TypeNote {
		item, resolveErr := r.GetByID(id, typ)
		if resolveErr != nil {
			return resolveErr
		}
		path = item.Path
	} else {
		path, err = r.pathForID(id, typ)
		if err != nil {
			return err
		}
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return os.RemoveAll(strings.TrimSuffix(path, ".md") + ".assets")
}

func (r *Repository) root(typ Type) (string, error) {
	switch typ {
	case TypePost:
		return r.PostsDir, nil
	case TypePage:
		return r.PagesDir, nil
	case TypeNote:
		return r.NotesDir, nil
	default:
		return "", errors.New("unknown content type")
	}
}

func (r *Repository) pathForID(id string, typ Type) (string, error) {
	if strings.Contains(id, "..") || filepath.IsAbs(id) {
		return "", errors.New("invalid content id")
	}
	root, err := r.root(typ)
	if err != nil {
		return "", err
	}
	if filepath.Ext(id) != ".md" {
		id += ".md"
	}
	return filepath.Join(root, filepath.FromSlash(id)), nil
}

func (r *Repository) newID(input SaveInput) (string, error) {
	date := input.Date
	if date.IsZero() {
		date = time.Now().UTC()
	}
	if input.Type == TypePage {
		slug := sanitizeSlug(input.Slug)
		if slug == "" {
			slug = sanitizeSlug(input.Title)
		}
		if slug == "" {
			return "", errors.New("page slug is required")
		}
		return slug, nil
	}
	return r.newDatedID(input.Type, date)
}

func (r *Repository) newDatedID(typ Type, date time.Time) (string, error) {
	root, err := r.root(typ)
	if err != nil {
		return "", err
	}
	year := date.Format("2006")
	prefix := date.Format("2006-01-02")
	for i := 1; ; i++ {
		id := filepath.ToSlash(filepath.Join(year, fmt.Sprintf("%s-%d", prefix, i)))
		path := filepath.Join(root, filepath.FromSlash(id)+".md")
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return id, nil
		}
	}
}

// sortItems 后台列表排序：三种类型一律只按时间倒序（最新在上），不使用标题。
// 同 Date 时 notes/posts 用文件名序号 seq 做稳定次级（seq 大=后建=在上），
// pages 无序号则用 ID 作稳定兜底；前台文章列表排序见 server.sortPublic。
func sortItems(items []*Item, typ Type) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if !a.Date.Equal(b.Date) {
			return a.Date.After(b.Date)
		}
		if typ == TypePage {
			return a.ID < b.ID
		}
		return seqFromID(a.ID) > seqFromID(b.ID)
	})
}

// seqFromID 从 ID 文件名解析序号（YYYY-MM-DD-seq-... 的第 4 段），失败返回 0。
func seqFromID(id string) int {
	_, name := filepath.Split(filepath.FromSlash(id))
	name = strings.TrimSuffix(name, ".md")
	parts := strings.Split(name, "-")
	if len(parts) < 4 {
		return 0
	}
	n, _ := strconv.Atoi(parts[3])
	return n
}

func sanitizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = safeSlug.ReplaceAllString(value, "-")
	return strings.Trim(value, "-")
}
