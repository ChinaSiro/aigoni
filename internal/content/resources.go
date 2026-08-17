package content

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const MaxAssetSize int64 = 20 << 20

const (
	draftCleanupInterval = 24 * time.Hour
	draftCleanupMaxAge   = 7 * 24 * time.Hour
)

var ErrAssetTooLarge = errors.New("asset is too large")

type AssetFile struct {
	Name     string
	Path     string
	Markdown string
	IsImage  bool
	IsCover  bool
	Size     int64
}

type ResourceService struct {
	repo        *Repository
	contentDir  string
	allowedExts []string
}

var safeAssetName = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func NewResourceService(repo *Repository, contentDir string, allowedExts []string) *ResourceService {
	return &ResourceService{
		repo:        repo,
		contentDir:  filepath.Clean(contentDir),
		allowedExts: normalizeExts(allowedExts),
	}
}

func (s *ResourceService) List(id string, typ Type) ([]AssetFile, *AssetFile, error) {
	item, err := s.repo.GetByID(id, typ)
	if err != nil {
		return nil, nil, err
	}
	return s.list(item)
}

func (s *ResourceService) list(item *Item) ([]AssetFile, *AssetFile, error) {
	dir := assetsDir(item)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	var files []AssetFile
	var cover *AssetFile
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || !s.allowed(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, nil, err
		}
		asset, err := s.assetFile(item, entry.Name(), info.Size())
		if err != nil {
			return nil, nil, err
		}
		if strings.HasPrefix(entry.Name(), "cover.") {
			asset.IsCover = true
			copy := asset
			cover = &copy
			continue
		}
		files = append(files, asset)
	}
	return files, cover, nil
}

func (s *ResourceService) Upload(id string, typ Type, header *multipart.FileHeader) (AssetFile, error) {
	name, err := s.safeName(header.Filename)
	if err != nil {
		return AssetFile{}, err
	}
	item, err := s.repo.GetByID(id, typ)
	if err != nil {
		return AssetFile{}, err
	}
	dir := assetsDir(item)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return AssetFile{}, err
	}
	name = nextFreeName(dir, name)
	return s.writeUpload(item, header, name)
}

func (s *ResourceService) UploadReader(id string, typ Type, filename string, src io.Reader) (AssetFile, error) {
	name, err := s.safeName(filename)
	if err != nil {
		return AssetFile{}, err
	}
	item, err := s.repo.GetByID(id, typ)
	if err != nil {
		return AssetFile{}, err
	}
	dir := assetsDir(item)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return AssetFile{}, err
	}
	name = nextFreeName(dir, name)
	return s.writeAsset(item, src, name)
}

func (s *ResourceService) UploadCover(id string, typ Type, header *multipart.FileHeader) (AssetFile, error) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(header.Filename), "."))
	if ext == "" || !slices.Contains(s.allowedExts, ext) {
		return AssetFile{}, errors.New("file type is not allowed")
	}
	item, err := s.repo.GetByID(id, typ)
	if err != nil {
		return AssetFile{}, err
	}
	dir := assetsDir(item)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return AssetFile{}, err
	}
	if err := removeCoverFiles(dir); err != nil {
		return AssetFile{}, err
	}
	asset, err := s.writeUpload(item, header, "cover."+ext)
	if err != nil {
		return AssetFile{}, err
	}
	item.CoverImage = asset.Path
	if _, err := s.repo.Save(saveInputFromItem(item)); err != nil {
		return AssetFile{}, err
	}
	return asset, nil
}

func (s *ResourceService) Delete(id string, typ Type, name string) error {
	name = filepath.Base(name)
	if name == "." || name == "" || strings.Trim(name, ".") == "" || name != safeAssetName.ReplaceAllString(name, "-") {
		return errors.New("invalid asset name")
	}
	item, err := s.repo.GetByID(id, typ)
	if err != nil {
		return err
	}
	if strings.HasPrefix(name, "cover.") || strings.EqualFold(filepath.Ext(name), ".md") {
		return errors.New("invalid asset name")
	}
	path := filepath.Join(assetsDir(item), name)
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("cannot delete directory")
	}
	return os.Remove(path)
}

func (s *ResourceService) DeleteCover(id string, typ Type) error {
	item, err := s.repo.GetByID(id, typ)
	if err != nil {
		return err
	}
	if err := removeCoverFiles(assetsDir(item)); err != nil {
		return err
	}
	item.CoverImage = ""
	_, err = s.repo.Save(saveInputFromItem(item))
	return err
}

func (s *ResourceService) AllowedExts() []string {
	return append([]string(nil), s.allowedExts...)
}

// UploadDraft stores an asset under content/.drafts/<token>.assets before a Markdown file exists.
// CreateDraft reserves a temporary asset directory and returns its opaque token.
func (s *ResourceService) CreateDraft() (string, error) {
	for range 10 {
		token, err := newDraftToken()
		if err != nil {
			return "", err
		}
		dir := filepath.Join(s.contentDir, ".drafts", token+".assets")
		err = os.Mkdir(dir, 0o755)
		if err == nil {
			return token, nil
		}
		if !errors.Is(err, os.ErrExist) {
			if errors.Is(err, os.ErrNotExist) {
				if mkdirErr := os.MkdirAll(filepath.Dir(dir), 0o755); mkdirErr != nil {
					return "", mkdirErr
				}
				continue
			}
			return "", err
		}
	}
	return "", errors.New("failed to allocate draft token")
}

// ListDraft returns temporary assets and the optional draft cover.
func (s *ResourceService) ListDraft(token string) ([]AssetFile, *AssetFile, error) {
	item, err := s.draftItem(token)
	if err != nil {
		return nil, nil, err
	}
	return s.list(item)
}

func (s *ResourceService) UploadDraft(token string, header *multipart.FileHeader) (AssetFile, error) {
	name, err := s.safeName(header.Filename)
	if err != nil {
		return AssetFile{}, err
	}
	item, err := s.draftItem(token)
	if err != nil {
		return AssetFile{}, err
	}
	dir := assetsDir(item)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return AssetFile{}, err
	}
	name = nextFreeName(dir, name)
	return s.writeUpload(item, header, name)
}

func (s *ResourceService) UploadDraftCover(token string, header *multipart.FileHeader) (AssetFile, error) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(header.Filename), "."))
	if ext == "" || !slices.Contains(s.allowedExts, ext) {
		return AssetFile{}, errors.New("file type is not allowed")
	}
	item, err := s.draftItem(token)
	if err != nil {
		return AssetFile{}, err
	}
	dir := assetsDir(item)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return AssetFile{}, err
	}
	if err := removeCoverFiles(dir); err != nil {
		return AssetFile{}, err
	}
	return s.writeUpload(item, header, "cover."+ext)
}

func (s *ResourceService) DeleteDraft(token, name string) error {
	item, err := s.draftItem(token)
	if err != nil {
		return err
	}
	name = filepath.Base(name)
	if name == "." || name == "" || strings.Trim(name, ".") == "" || name != safeAssetName.ReplaceAllString(name, "-") {
		return errors.New("invalid asset name")
	}
	return os.Remove(filepath.Join(assetsDir(item), name))
}

func (s *ResourceService) DeleteDraftCover(token string) error {
	item, err := s.draftItem(token)
	if err != nil {
		return err
	}
	return removeCoverFiles(assetsDir(item))
}

// CommitDraft moves temporary assets beside the saved Markdown and rewrites temporary URLs.
func (s *ResourceService) CommitDraft(token string, item *Item, body, cover string) (string, string, error) {
	draft, err := s.draftItem(token)
	if err != nil {
		return body, cover, err
	}
	from := assetsDir(draft)
	if _, err := os.Stat(from); errors.Is(err, os.ErrNotExist) {
		return body, cover, nil
	} else if err != nil {
		return body, cover, err
	}
	to := assetsDir(item)
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return body, cover, err
	}
	if _, err := os.Stat(to); err == nil {
		return body, cover, errors.New("saved content already has an assets directory")
	} else if !errors.Is(err, os.ErrNotExist) {
		return body, cover, err
	}
	if err := os.Rename(from, to); err != nil {
		return body, cover, err
	}
	oldPrefix, err := s.assetURLPrefix(draft)
	if err != nil {
		return body, cover, err
	}
	newPrefix, err := s.assetURLPrefix(item)
	if err != nil {
		return body, cover, err
	}
	return strings.ReplaceAll(body, oldPrefix, newPrefix), strings.ReplaceAll(cover, oldPrefix, newPrefix), nil
}

// StartDraftCleanup removes stale abandoned draft asset directories until ctx is canceled.
func (s *ResourceService) StartDraftCleanup(ctx context.Context) {
	s.startDraftCleanup(ctx, draftCleanupInterval, time.Now)
}

func (s *ResourceService) startDraftCleanup(ctx context.Context, interval time.Duration, now func() time.Time) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			return
		default:
		}

		s.cleanupExpiredDrafts(now())

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case tick := <-ticker.C:
				s.cleanupExpiredDrafts(tick)
			}
		}
	}()
	return done
}

func (s *ResourceService) cleanupExpiredDrafts(now time.Time) int {
	return s.cleanupExpiredDraftsWithRemove(now, os.RemoveAll)
}

func (s *ResourceService) cleanupExpiredDraftsWithRemove(now time.Time, remove func(string) error) int {
	draftsDir := filepath.Join(s.contentDir, ".drafts")
	info, err := os.Lstat(draftsDir)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return 0
	}

	entries, err := os.ReadDir(draftsDir)
	if err != nil {
		return 0
	}
	cutoff := now.Add(-draftCleanupMaxAge)
	removed := 0
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(name, ".assets") {
			continue
		}
		token := strings.TrimSuffix(name, ".assets")
		if !ValidDraftToken(token) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		path, ok := draftAssetCleanupPath(draftsDir, name)
		if !ok {
			continue
		}
		if err := remove(path); err != nil {
			continue
		}
		removed++
	}
	return removed
}

func draftAssetCleanupPath(draftsDir, name string) (string, bool) {
	root := filepath.Clean(draftsDir)
	path := filepath.Join(root, name)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", false
	}
	return path, true
}

func (s *ResourceService) draftItem(token string) (*Item, error) {
	if !ValidDraftToken(token) {
		return nil, errors.New("invalid draft token")
	}
	return &Item{Path: filepath.Join(s.contentDir, ".drafts", token+".md")}, nil
}

func (s *ResourceService) assetURLPrefix(item *Item) (string, error) {
	rel, err := filepath.Rel(s.contentDir, assetsDir(item))
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", errors.New("asset path escapes content root")
	}
	return "/assets/" + filepath.ToSlash(rel), nil
}

func newDraftToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// ValidDraftToken reports whether token can name a draft asset directory.
func ValidDraftToken(token string) bool {
	if len(token) != 32 {
		return false
	}
	for _, r := range token {
		if !((r >= 'a' && r <= 'f') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func (s *ResourceService) writeUpload(item *Item, header *multipart.FileHeader, name string) (AssetFile, error) {
	src, err := header.Open()
	if err != nil {
		return AssetFile{}, err
	}
	defer src.Close()
	return s.writeAsset(item, src, name)
}

func (s *ResourceService) writeAsset(item *Item, src io.Reader, name string) (AssetFile, error) {
	dir := assetsDir(item)
	dstPath := filepath.Join(dir, name)
	tmp, err := os.CreateTemp(dir, "."+name+"-*")
	if err != nil {
		return AssetFile{}, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	written, copyErr := io.Copy(tmp, io.LimitReader(src, MaxAssetSize+1))
	if copyErr != nil {
		tmp.Close()
		return AssetFile{}, copyErr
	}
	if written > MaxAssetSize {
		tmp.Close()
		return AssetFile{}, ErrAssetTooLarge
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return AssetFile{}, err
	}
	if err := tmp.Close(); err != nil {
		return AssetFile{}, err
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return AssetFile{}, fmt.Errorf("replace asset: %w", err)
	}
	return s.assetFile(item, name, written)
}

func (s *ResourceService) safeName(name string) (string, error) {
	base := filepath.Base(name)
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(base), "."))
	if ext == "" || !slices.Contains(s.allowedExts, ext) {
		return "", errors.New("file type is not allowed")
	}
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	stem = strings.Trim(safeAssetName.ReplaceAllString(strings.TrimSpace(stem), "-"), ".-")
	if stem == "" {
		stem = "asset"
	}
	return stem + "." + ext, nil
}

func (s *ResourceService) allowed(name string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	return slices.Contains(s.allowedExts, ext)
}

func (s *ResourceService) assetFile(item *Item, name string, size int64) (AssetFile, error) {
	full := filepath.Join(assetsDir(item), name)
	rel, err := filepath.Rel(s.contentDir, full)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return AssetFile{}, errors.New("asset path escapes content root")
	}
	path := "/assets/" + filepath.ToSlash(rel)
	return AssetFile{
		Name:     name,
		Path:     path,
		Markdown: "![描述](" + path + ")",
		IsImage:  isImageExt(name),
		Size:     size,
	}, nil
}

func assetsDir(item *Item) string {
	return strings.TrimSuffix(item.Path, ".md") + ".assets"
}

func nextFreeName(dir, name string) string {
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		candidate := stem + "-" + strconv.Itoa(i) + ext
		if _, err := os.Stat(filepath.Join(dir, candidate)); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
	}
}

func removeCoverFiles(dir string) error {
	matches, err := filepath.Glob(filepath.Join(dir, "cover.*"))
	if err != nil {
		return err
	}
	for _, path := range matches {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}
	return nil
}

func saveInputFromItem(item *Item) SaveInput {
	return SaveInput{
		ID:          item.ID,
		Type:        item.Type,
		Title:       item.Title,
		Description: item.Description,
		Date:        item.Date,
		Publish:     item.Publish,
		Slug:        item.Slug,
		Category:    item.Category,
		Tags:        item.Tags,
		CoverImage:  item.CoverImage,
		TOC:         item.TOC,
		Template:    item.Template,
		SourceURL:   item.SourceURL,
		WikiStatus:  item.WikiStatus,
		WikiHash:    item.WikiHash,
		Weight:      item.Weight,
		Body:        item.Body,
	}
}

func normalizeExts(exts []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, ext := range exts {
		ext = strings.ToLower(strings.Trim(strings.TrimSpace(ext), "."))
		if ext != "" && !seen[ext] {
			seen[ext] = true
			out = append(out, ext)
		}
	}
	return out
}

func isImageExt(name string) bool {
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(name), ".")) {
	case "jpg", "jpeg", "png", "webp", "gif":
		return true
	default:
		return false
	}
}
