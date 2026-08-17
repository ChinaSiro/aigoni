package server

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
)

// settingsAssetSubdir 为站点级静态资源（logo、作者头像）在 uploads 目录下的子目录。
const settingsAssetSubdir = "site"

// settingsAssetKindPrefix 把前端上传时携带的 kind 归一为文件名前缀，避免不可信输入污染文件名。
func settingsAssetKindPrefix(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "logo":
		return "logo"
	case "avatar":
		return "avatar"
	default:
		return "site"
	}
}

// fixedSettingsAssetName 固定文件名 {kind}.{ext}，扩展名经白名单校验。
// 站点级资源（logo/头像）单一，固定名覆盖，避免旧图残留。
func fixedSettingsAssetName(kind, filename string, allowedExts []string) (string, error) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	if ext == "" || !slices.Contains(allowedExts, ext) {
		return "", errors.New("file type is not allowed")
	}
	return settingsAssetKindPrefix(kind) + "." + ext, nil
}
