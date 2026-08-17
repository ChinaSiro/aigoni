package aigoni

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed config.yaml.example .env.example frontend
var runtimeFiles embed.FS

// EnsureRuntimeFiles writes the packaged default runtime files when they are absent.
func EnsureRuntimeFiles(root string) error {
	if err := ensureRuntimeFileFromEmbed(root, "config.yaml", "config.yaml.example"); err != nil {
		return err
	}
	return ensureRuntimeFile(root, ".env.example")
}

// FrontendFS returns an embedded frontend build directory such as web or admin.
func FrontendFS(name string) (fs.FS, error) {
	name = strings.Trim(strings.TrimSpace(name), "/")
	if name == "" || strings.Contains(name, "/") {
		return nil, fmt.Errorf("invalid frontend name %q", name)
	}
	return fs.Sub(runtimeFiles, "frontend/"+name)
}

// EnsureRuntimeDirs creates runtime data directories when they are absent.
func EnsureRuntimeDirs(root string, dirs ...string) error {
	for _, dir := range dirs {
		if err := ensureRuntimeEmptyDir(root, dir); err != nil {
			return err
		}
	}
	return nil
}

func ensureRuntimeFile(root, name string) error {
	return ensureRuntimeFileFromEmbed(root, name, name)
}

func ensureRuntimeFileFromEmbed(root, name, embedName string) error {
	target := runtimePath(root, name)
	info, err := os.Stat(target)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("%s exists and is a directory", target)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	data, err := runtimeFiles.ReadFile(embedName)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o644)
}

func ensureRuntimeDir(root, name string) error {
	target := runtimePath(root, name)
	info, err := os.Stat(target)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s exists and is not a directory", target)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, "."+filepath.Base(target)+"-")
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tmp)
		}
	}()

	if err := copyRuntimeDir(tmp, name); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		if info, statErr := os.Stat(target); statErr == nil && info.IsDir() {
			return nil
		}
		return err
	}
	cleanup = false
	return nil
}

func ensureRuntimeEmptyDir(root, name string) error {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	target := runtimePath(root, name)
	info, err := os.Stat(target)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s exists and is not a directory", target)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.MkdirAll(target, 0o755)
}

func runtimePath(root, name string) string {
	name = strings.TrimSpace(name)
	if filepath.IsAbs(name) {
		return filepath.Clean(name)
	}
	return filepath.Join(root, filepath.FromSlash(name))
}

func copyRuntimeDir(target, source string) error {
	return fs.WalkDir(runtimeFiles, source, func(sourcePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if sourcePath == source {
			return nil
		}
		rel := strings.TrimPrefix(sourcePath, source+"/")
		targetPath := filepath.Join(target, filepath.FromSlash(rel))
		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		data, err := runtimeFiles.ReadFile(sourcePath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(targetPath, data, 0o644)
	})
}
