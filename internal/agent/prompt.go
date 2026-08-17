package agent

import (
	"embed"
	"os"
	"path/filepath"
	"strings"
)

//go:embed prompt.md
var promptFS embed.FS

// LoadPrompt returns the embedded fallback instruction text used by ChatModelAgent.
func LoadPrompt() (string, error) {
	data, err := promptFS.ReadFile("prompt.md")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// LoadPromptFromRoot reads internal/agent/prompt.md from the repository when present.
func LoadPromptFromRoot(root string) (string, error) {
	path := filepath.Join(root, "internal", "agent", "prompt.md")
	data, err := os.ReadFile(path)
	if err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	if os.IsNotExist(err) {
		return LoadPrompt()
	}
	return "", err
}
