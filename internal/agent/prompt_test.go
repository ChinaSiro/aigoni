package agent

import (
	"strings"
	"testing"
)

func TestLoadPrompt(t *testing.T) {
	prompt, err := LoadPrompt()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "LLM Wiki") {
		t.Fatalf("unexpected prompt: %.80q", prompt)
	}
}
